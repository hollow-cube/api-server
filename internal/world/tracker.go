package world

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hollow-cube/api-server/internal/db"
	"github.com/hollow-cube/api-server/internal/pkg/model"
	"github.com/hollow-cube/api-server/internal/pkg/natsutil"
	"github.com/hollow-cube/api-server/internal/pkg/server"
	"github.com/nats-io/nats.go/jetstream"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var ErrNoServerAvailable = errors.New("no server available")

var (
	worldMgmtStream         = "MAP_WORLD_MANAGEMENT"
	worldMgmtConsumerConfig = jetstream.ConsumerConfig{
		Name:          "map-world-processor",
		Durable:       "map-world-processor",
		FilterSubject: "map-world.>",
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       10 * time.Second,
		MaxAckPending: 1,
		MaxDeliver:    3,
	}
)

// Tracker is responsible for keeping track of all active map worlds on the server.
type Tracker struct {
	log *zap.SugaredLogger

	sessionStore  *db.Queries
	serverTracker *server.Tracker
	jetStream     *natsutil.JetStreamWrapper
}

type TrackerParams struct {
	fx.In
	Log *zap.SugaredLogger

	SessionStore  *db.Queries
	ServerTracker *server.Tracker
	JetStream     *natsutil.JetStreamWrapper
}

func NewTracker(lc fx.Lifecycle, p TrackerParams) (*Tracker, error) {
	err := p.JetStream.UpsertStream(context.Background(), jetstream.StreamConfig{
		Name:       "MAP_WORLD_MANAGEMENT",
		Subjects:   []string{"map-world.>"},
		Retention:  jetstream.LimitsPolicy,
		Storage:    jetstream.FileStorage,
		MaxAge:     5 * time.Minute,
		Duplicates: time.Minute,
	})
	if err != nil {
		return nil, err
	}

	t := &Tracker{
		log:           p.Log,
		sessionStore:  p.SessionStore,
		serverTracker: p.ServerTracker,
		jetStream:     p.JetStream,
	}

	cons, err := p.JetStream.Subscribe(context.Background(), worldMgmtStream, worldMgmtConsumerConfig, t.handleMapWorldUpdate)
	if err != nil {
		return nil, err
	}
	lc.Append(fx.StartStopHook(cons.Start, cons.Stop))

	return t, nil
}

func (t *Tracker) FindServerForMap(ctx context.Context, _ string) (*db.ServerState, error) {
	// TODO: Selecting a map server needs to be a lot more complicated.
	mapServers, err := t.serverTracker.GetActiveServersWithRole(ctx, "map", "")
	if err != nil {
		return nil, fmt.Errorf("failed to get active servers: %w", err)
	} else if len(mapServers) == 0 {
		return nil, ErrNoServerAvailable
	}

	state, err := t.serverTracker.GetState(ctx, mapServers[0])
	if err != nil {
		return nil, fmt.Errorf("failed to get server state: %w", err)
	}

	return state, nil
}

// DestroyAndWait destroys all worlds running the given mapID no matter the world type
// and waits for all servers to be reported gone before returning.
func (t *Tracker) DestroyAndWait(ctx context.Context, mapID string) error {
	// Always send out a drain just in case we are racing a world creation.
	// The server-side consumer processes messages up to 1m prior, so it will still be destroyed by this.
	err := t.jetStream.PublishJSONAsync(ctx, model.MapUpdateMessage{
		Action:      model.MapUpdate_Drain,
		ID:          mapID,
		DrainReason: new("verification"),
	})
	if err != nil {
		return fmt.Errorf("failed to publish map drain message: %w", err)
	}

	// Wait until they are all gone with a sanity limit of 30s
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		worlds, err := t.sessionStore.GetWorldsForMap(ctx, mapID)
		if err != nil {
			return fmt.Errorf("failed to get worlds for map: %w", err)
		}
		println("FOUND", len(worlds), "WORLDS ACTIVE")
		if len(worlds) == 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
			// loop again
		}
	}
}

func (t *Tracker) handleMapWorldUpdate(ctx context.Context, msg jetstream.Msg) error {
	var message UpdateMessage
	if err := json.Unmarshal(msg.Data(), &message); err != nil {
		return fmt.Errorf("failed to decode map update message: %w", err)
	}

	var err error
	switch message.Action {
	case ActionCreated:
		err = t.handleMapWorldCreated(ctx, message)
	case ActionDestroyed:
		err = t.handleMapWorldDestroyed(ctx, message)
	}
	if err != nil {
		return err
	}

	return msg.Ack()
}

func (t *Tracker) handleMapWorldCreated(ctx context.Context, msg UpdateMessage) error {
	err := t.sessionStore.InsertMapWorld(ctx, msg.WorldID, msg.MapID, msg.ServerID)
	if err != nil {
		// We dont really have a fail recourse here, just log for now.
		t.log.Errorw("failed to insert map world", "error", err)
	}

	return nil
}

func (t *Tracker) handleMapWorldDestroyed(ctx context.Context, msg UpdateMessage) error {
	err := t.sessionStore.DeleteMapWorld(ctx, msg.WorldID)
	if err != nil {
		t.log.Errorw("failed to delete map world", "error", err)
	}

	return nil
}
