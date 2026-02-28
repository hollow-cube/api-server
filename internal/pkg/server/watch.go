package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/hollow-cube/api-server/internal/db"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"
	coreV1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection"
)

// TODO: FIND A NEW HOME FOR THE PLAYER COUNT REPORTER

const (
	roleLabel              = "mapmaker.hollowcube.net/role"
	anyServerLabelSelector = roleLabel + ` in (hub,map,map-isolate)`
)

var otelTracer = otel.Tracer("github.com/hollow-cube/api-server/internal/pkg/server")

func (t *Tracker) podWatchLeadershipLoop() {
	for {
		select {
		case <-t.ctx.Done():
			return
		default: // Do nothing
		}

		leaderelection.RunOrDie(t.ctx, leaderelection.LeaderElectionConfig{
			Lock:          t.k8sLock,
			LeaseDuration: 30 * time.Second,
			RenewDeadline: 15 * time.Second,
			RetryPeriod:   2 * time.Second,
			Callbacks: leaderelection.LeaderCallbacks{
				OnStartedLeading: t.podWatchReadLoop,
				OnStoppedLeading: func() {
					// Noop
				},
			},
		})
	}
}

func (t *Tracker) podWatchReadLoop(ctx context.Context) {
	t.wg.Add(1)
	defer t.wg.Done()

	// Sync the entire state, in case updates happened between the last pod death and when we acked the lease.
	t.log.Infow("Leadership acquired, syncing current state.")

	_, err := t.fullPodSync(ctx)
	if err != nil {
		t.log.Errorw("failed to sync pods", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			break
		case <-time.After(5 * time.Second): // Wait a bit before re-syncing
		}

		_, err := t.fullPodSync(ctx)
		if errors.Is(err, context.Canceled) {
			break
		} else if err != nil {
			t.log.Errorw("failed to sync pods", "error", err)
			continue
		}
	}

	//t.log.Infow("Pod sync complete, starting watch", "resourceVersion", resourceVersion)
	//watchOptions := metaV1.ListOptions{ResourceVersion: resourceVersion, LabelSelector: anyServerLabelSelector}
	//watchInterface, err := t.k8s.CoreV1().Pods(t.K8sNamespace).Watch(ctx, watchOptions)
	//if err != nil {
	//	t.log.Errorw("failed to watch pods", "error", err)
	//	return
	//}
	//defer watchInterface.Stop()
	//
	//for event := range watchInterface.ResultChan() {
	//	pod, ok := event.Object.(*coreV1.Pod)
	//	if !ok {
	//		t.log.Errorw("unexpected object type", "type", fmt.Sprintf("%T", event.Object), "value", event.Object)
	//		continue
	//	}
	//
	//	switch event.Type {
	//	case watch.Added:
	//		err = t.handlePodAdded(ctx, pod)
	//	case watch.Modified:
	//		err = t.handlePodUpdated(ctx, pod)
	//	case watch.Deleted:
	//		err = t.handlePodRemoved(ctx, pod.Name)
	//	}
	//	if err != nil {
	//		t.log.Errorw("failed to handle event", "error", err)
	//		return
	//	}
	//}

	t.log.Infow("Pod watch loop exited.")
}

// fullPodSync fetches the entire list of pods matching our query and diffs
// it against the redis state, updating as necessary.
//
// After this call returns, the redis state should be completely in-sync with
// the returned resource version.
//
// The return value is the kubernetes ResourceVersion of the List call.
func (t *Tracker) fullPodSync(ctx context.Context) (string, error) {
	ctx, span := otelTracer.Start(ctx, "server.fullPodSync")
	defer span.End()
	defer t.updateMapIsolateMetrics(ctx)

	// Fetch all matching pods and the server keys from storage
	listOptions := metaV1.ListOptions{LabelSelector: anyServerLabelSelector}
	pods, err := t.k8s.CoreV1().Pods(t.K8sNamespace).List(ctx, listOptions)
	if err != nil {
		return "", fmt.Errorf("failed to fetch pods: %w", err)
	}

	servers, err := t.AllServerKeys(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to fetch existing servers: %w", err)
	}
	serversByName := make(map[string]bool, len(servers))
	for _, server := range servers {
		serversByName[server] = true
	}

	podNames := make([]string, len(pods.Items))
	for i, pod := range pods.Items {
		podNames[i] = pod.Name
	}

	// Compare the pods to the servers
	for _, pod := range pods.Items {
		if ok := serversByName[pod.Name]; ok {
			err = t.handlePodUpdated(ctx, &pod) // Pod is updated
		} else {
			err = t.handlePodAdded(ctx, &pod) // Pod is new
		}
		if err != nil {
			return "", fmt.Errorf("failed to update pod state: %w", err)
		}

		delete(serversByName, pod.Name)
	}

	// All remaining entries in servers are gone, so remove them
	for server := range serversByName {
		err = t.handlePodRemoved(ctx, server)
		if err != nil {
			return "", fmt.Errorf("failed to remove pod state: %w", err)
		}
	}

	// Remove all players who are not in a server for > 30s
	players, err := t.queries.ListTimedOutPlayers(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to fetch players: %w", err)
	}
	for _, playerId := range players {
		_, err = t.players.DeleteSession(ctx, playerId)
		if err != nil {
			t.log.Errorw("failed to delete expired player session", "error", err)
		}
	}

	return pods.ResourceVersion, nil
}

func (t *Tracker) handlePodAdded(ctx context.Context, pod *coreV1.Pod) error {
	podName := pod.Name
	// Refetch the pod to get the cluster ip
	p := t.k8s.CoreV1().Pods(t.K8sNamespace)
	pod, err := p.Get(ctx, pod.Name, metaV1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return t.handlePodRemoved(ctx, podName)
	} else if err != nil {
		return fmt.Errorf("failed to fetch pod: %w", err)
	}

	status := getPodStatus(pod)
	zap.S().Infow("Server added", "id", pod.Name, "status", status)

	// Locking here is kinda unnecessary because we know for sure the server doesnt exist and the actual insert
	// here is atomic. But might as well make the code clearer that we must lock while modifying a server state.
	//ctx, cancel, err := t.locker.WithContext(ctx, pod.Name)
	//if err != nil {
	//	return fmt.Errorf("failed to acquire lock: %w", err)
	//}
	//defer cancel()

	role := pod.Labels[roleLabel]
	_, err = t.queries.InsertServerState(ctx, db.InsertServerStateParams{
		ID:        pod.Name,
		Role:      role,
		Status:    int32(status),
		ClusterIp: pod.Status.PodIP,
	})
	if err != nil {
		return fmt.Errorf("failed to insert server state: %w", err)
	}

	return nil
}

func (t *Tracker) handlePodUpdated(ctx context.Context, pod *coreV1.Pod) error {
	newStatus := getPodStatus(pod)

	for _, container := range pod.Status.ContainerStatuses {
		if container.Name != "map-isolate" && container.Name != "mapmaker-node" {
			continue
		}

		// If the pod is terminated we can delete it. This will drop the record on the next polling run.
		if container.State.Terminated != nil {
			err := deleteMapServerPod(ctx, t.k8s, pod.Name)
			if err != nil {
				t.log.Errorw("failed to delete pod", "error", err)
			}
			return nil
		}

	}

	//ctx, cancel, err := t.locker.WithContext(ctx, pod.Name)
	//if err != nil {
	//	return fmt.Errorf("failed to acquire lock: %w", err)
	//}
	//defer cancel()

	// Get current actual state of the server
	state, err := t.GetState(ctx, pod.Name)
	if err != nil {
		return fmt.Errorf("failed to fetch server state: %w", err)
	} else if state == nil {
		// This is technically an impossible state, but for now lets just make it "work"
		//cancel()
		return t.handlePodAdded(ctx, pod)
	}

	var changed bool
	if state.ClusterIp != pod.Status.PodIP {
		t.log.Infow("server cluster ip changed", "from", state.ClusterIp, "to", pod.Status.PodIP)
		state.ClusterIp = pod.Status.PodIP
		changed = true
	}
	// Note that we allow a server to go from draining back to ready. it can happen if a pod is unresponsive for a moment and comes back.
	if newStatus == Ready && (state.StatusV2 == string(Starting) || (state.Role != "map-isolate" && state.StatusV2 == string(Draining))) {
		state.StatusV2 = string(Active)
		state.StatusSince = time.Now()
		changed = true
	} else if newStatus == NotReady && state.StatusV2 == string(Active) {
		state.StatusV2 = string(Draining)
		state.StatusSince = time.Now()
		changed = true
	}
	if changed {
		err = t.queries.UpdateServerState(ctx, db.UpdateServerStateParams{
			ID:          pod.Name,
			Status:      int32(newStatus),
			ClusterIp:   pod.Status.PodIP,
			StatusV2:    state.StatusV2,
			StatusSince: state.StatusSince,
		})
		if err != nil {
			return fmt.Errorf("failed to update server state: %w", err)
		}
	}
	if state.StatusV2 == string(Starting) && state.StartTime.Before(time.Now().Add(-30*time.Second)) {
		return nil
	}

	// Update players last_seen time
	players, err := t.queryServerPlayers(ctx, pod.Status.PodIP)
	if err != nil && state.StartTime.Before(time.Now().Add(-30*time.Second)) {
		t.log.Errorw("failed to query server players, destroying", "error", err)
		if err = deleteMapServerPod(ctx, t.k8s, pod.Name); err != nil {
			t.log.Errorw("failed to delete pod", "error", err)
		}
		return nil
	}
	if players != nil && len(players.Players) > 0 {
		if err = t.queries.UpdatePlayerLastSeenByServer(ctx, pod.Name, players.Players); err != nil {
			return fmt.Errorf("failed to update player sessions: %w", err)
		}
	} else if state.Role == "map-isolate" {
		// If this server is >30s active and empty, move it to draining by recording internally and telling the server
		// TODO: this means we instantly mark draining currently. in the future we should wait a bit.
		// TODO: need to tell the server its draining
		if state.StatusV2 == string(Active) && state.StatusSince.Before(time.Now().Add(-30*time.Second)) {
			zap.S().Infow("Server draining", "id", pod.Name)
			if err = t.queries.UpdateServerStatus(ctx, pod.Name, string(Draining)); err != nil {
				return fmt.Errorf("failed to update server state: %w", err)
			}
		} else
		// If this server is >30s draining and still empty, delete it.
		if state.StatusV2 == string(Draining) && state.StatusSince.Before(time.Now().Add(-30*time.Second)) {
			zap.S().Infow("Server drained", "id", pod.Name)
			if err = deleteMapServerPod(ctx, t.k8s, pod.Name); err != nil {
				t.log.Errorw("failed to delete pod", "error", err)
			}
			return nil
		}
	}

	return nil
}

type serverPlayers struct {
	Players []string `json:"players"`
}

func (t *Tracker) queryServerPlayers(ctx context.Context, clusterIp string) (*serverPlayers, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+clusterIp+":9124/players", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch players: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch players: %s", resp.Status)
	}

	var players serverPlayers
	if err := json.NewDecoder(resp.Body).Decode(&players); err != nil {
		return nil, fmt.Errorf("failed to decode players: %w", err)
	}

	return &players, nil
}

func (t *Tracker) handlePodRemoved(ctx context.Context, id string) error {
	zap.S().Infow("Server deleted", "id", id)

	// TODO: do we want to delete maps and players who are currently on this server?
	// At this point we know the server is for sure gone, but there could be a race here where
	// the player has not yet been transfered to a different server yet.

	if err := t.queries.DeleteServerState(ctx, id); err != nil {
		return fmt.Errorf("failed to delete server state: %w", err)
	}
	return nil
}

func getPodStatus(pod *coreV1.Pod) Status {
	if pod.DeletionTimestamp != nil {
		return NotReady // Its in the process of being deleted. Never considered in our ready state.
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == coreV1.PodReady {
			if condition.Status == coreV1.ConditionTrue {
				return Ready
			} else {
				return NotReady
			}
		}
	}
	return NotReady
}

func (t *Tracker) updateMapIsolateMetrics(ctx context.Context) {
	counts, err := t.queries.CountMapIsolatesByStatus(ctx)
	if err != nil {
		t.log.Errorw("failed to count map isolates by status", "error", err)
		return
	}

	// Reset all states to 0 first - they may not be in the row counts
	mapIsolateCount.WithLabelValues(string(Starting)).Set(0)
	mapIsolateCount.WithLabelValues(string(Active)).Set(0)
	mapIsolateCount.WithLabelValues(string(Draining)).Set(0)

	// Set actual counts
	for _, row := range counts {
		mapIsolateCount.WithLabelValues(row.StatusV2).Set(float64(row.Count))
	}
}
