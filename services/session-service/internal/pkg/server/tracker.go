package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/go-github/v56/github"
	"github.com/google/uuid"
	mapService "github.com/hollow-cube/hc-services/services/map-service/api/v3/intnl"
	"github.com/hollow-cube/hc-services/services/session-service/config"
	"github.com/hollow-cube/hc-services/services/session-service/internal/db"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/player"
	"github.com/redis/rueidis"
	"github.com/redis/rueidis/rueidislock"
	"go.uber.org/fx"
	"go.uber.org/zap"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// Some notes about how server management works:
// * Only the leader may add or remove servers from the cache (in pg).
// * The content of a server may be modified by non-leaders, but any operator
//   MUST have a lock on that server to edit it.
// * Reading may be done without a lock if a consistent state is not required.

type TrackerParams struct {
	fx.In

	Config     *config.Config
	Queries    *db.Queries
	Redis      rueidis.Client
	Kubernetes *kubernetes.Clientset
	Players    *player.Tracker
	Maps       mapService.ClientWithResponsesInterface
	GitHub     *github.Client
}

type Tracker struct {
	log     *zap.SugaredLogger
	queries *db.Queries
	players *player.Tracker
	locker  rueidislock.Locker
	gh      *github.Client

	maps mapService.ClientWithResponsesInterface

	isolateConfig *config.MapIsolate

	ctx       context.Context
	cancelCtx func()
	wg        *sync.WaitGroup

	K8sNamespace string
	k8s          *kubernetes.Clientset
	k8sLock      resourcelock.Interface
}

func NewTracker(p TrackerParams) (*Tracker, error) {
	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	hostname, _ := os.Hostname()

	locker, err := rueidislock.NewLocker(rueidislock.LockerOption{
		ClientBuilder: func(_ rueidis.ClientOption) (rueidis.Client, error) {
			return p.Redis, nil
		},
		KeyMajority:    1,
		NoLoopTracking: true,
		KeyPrefix:      "sess:server_v3:",
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create locker: %w", err)
	}

	var k8sLock resourcelock.Interface
	if p.Config.Kubernetes.Namespace != "disabled" {
		k8sLock = &resourcelock.LeaseLock{
			LeaseMeta: metaV1.ObjectMeta{
				Name:      "session-service-leader",
				Namespace: p.Config.Kubernetes.Namespace,
			},
			Client: p.Kubernetes.CoordinationV1(),
			LockConfig: resourcelock.ResourceLockConfig{
				Identity: hostname,
			},
		}
	}

	return &Tracker{
		log:     zap.S(),
		queries: p.Queries,
		players: p.Players,
		locker:  locker,
		gh:      p.GitHub,
		ctx:     ctx, cancelCtx: cancel, wg: wg,
		K8sNamespace:  p.Config.Kubernetes.Namespace,
		k8s:           p.Kubernetes,
		k8sLock:       k8sLock,
		isolateConfig: &p.Config.MapIsolate,
		maps:          p.Maps,
	}, nil
}

func (t *Tracker) Start(_ context.Context) error {
	go t.podWatchLeadershipLoop()
	return nil
}

func (t *Tracker) Stop(_ context.Context) error {
	t.cancelCtx()
	t.wg.Wait() // This will only be 1 if we are currently the leader, otherwise 0
	t.locker.Close()
	return nil
}

// AllServerKeys returns all the known servers in every state.
// Safe to call this without lock or leadership.
func (t *Tracker) AllServerKeys(ctx context.Context) ([]string, error) {
	return t.queries.ListServerIDs(ctx)
}

// GetActiveServersWithRole returns all the ready servers with the given role.
// Safe to call this without lock or leadership.
func (t *Tracker) GetActiveServersWithRole(ctx context.Context, role string, excluding string) ([]string, error) {
	return t.queries.ListServersWithRoleExcept(ctx, role, excluding)
}

// GetState returns the existing state (or nil, nil if not found) for a server.
// Safe to call without a lock or leadership if a consistent view is not required.
func (t *Tracker) GetState(ctx context.Context, id string) (*db.ServerState, error) {
	s, err := t.queries.GetServerState(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return s, nil
}

func (t *Tracker) findServerForMap(ctx context.Context, mapId string) (*db.ServerState, error) {
	s, err := t.queries.GetFirstServerStateByMap(ctx, mapId)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return s, nil
}

func (t *Tracker) AllocServerForMap(ctx context.Context, mapId, isolateOverride string) (*db.ServerState, error) {
	ctx, span := otelTracer.Start(ctx, "tracker.AllocServerForMap")
	defer span.End()

	existing, err := t.findServerForMap(ctx, mapId)
	if err != nil {
		return nil, fmt.Errorf("failed to find server for map: %w", err)
	} else if existing != nil {
		// We already have a server for this map, return it
		t.log.Infow("found existing server for map", "mapId", mapId, "server", existing.ID)
		return existing, nil
	}

	podName, resourceVersion, err := t.allocMapServerPod(ctx, mapId, isolateOverride)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate map server pod: %w", err)
	}

	// Eager insert the server & map states
	server, err := db.Tx(ctx, t.queries, func(ctx context.Context, queries *db.Queries) (*db.ServerState, error) {
		server, err := queries.InsertServerState(ctx, db.InsertServerStateParams{
			ID:        podName,
			Role:      "map-isolate",
			Status:    0,
			ClusterIp: "",
		})
		if err != nil {
			return nil, fmt.Errorf("failed to insert server state: %w", err)
		}
		if _, err = queries.InsertMapState(ctx, db.InsertMapStateParams{
			ID:     uuid.NewString(),
			MapID:  mapId,
			Server: podName,
			State:  "playing", // TODO not always playing
		}); err != nil {
			return nil, fmt.Errorf("failed to insert map state: %w", err)
		}

		return server, nil
	})
	if err != nil {
		return nil, err
	}

	t.log.Infow("Pod created, starting watch", "resourceVersion", resourceVersion)
	watchOptions := metaV1.ListOptions{ResourceVersion: resourceVersion, FieldSelector: fmt.Sprintf("metadata.name=%s", podName)}
	watchInterface, err := t.k8s.CoreV1().Pods(t.K8sNamespace).Watch(ctx, watchOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to watch pods: %w", err)
	}
	defer watchInterface.Stop()

	// Wait to get the IP
	for event := range watchInterface.ResultChan() {
		pod, ok := event.Object.(*coreV1.Pod)
		if !ok || pod.Name != podName {
			continue // DNC about this event
		}
		if event.Type == watch.Deleted {
			return nil, fmt.Errorf("pod failed to start successfully")
		} else if event.Type == watch.Modified {
			newStatus := getPodStatus(pod)

			server.ClusterIp = pod.Status.PodIP
			zap.S().Infow("pod update", "name", podName, "status", newStatus, "ip", pod.Status.PodIP)
		}

		// Check if we have the required server fields
		if server.ClusterIp != "" {
			break
		}
	}

	// Wait for the ready endpoint to respond
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	func() {
		ctx, span := otelTracer.Start(ctx, "tracker.AllocServerForMap.waitForReadyEndpoint")
		for {
			if getReadyEndpoint(ctx, server.ClusterIp) {
				server.StatusV2 = string(Active)
				server.StatusSince = time.Now()
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		span.End()
	}()

	if server.StatusV2 != string(Active) {
		return nil, fmt.Errorf("server failed to become ready")
	}

	return server, nil
}

func getReadyEndpoint(ctx context.Context, ip string) bool {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s:9124/ready", ip), nil)
	if err != nil {
		return false
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	_ = res.Body.Close()
	return res.StatusCode == http.StatusOK

}
