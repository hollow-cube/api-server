package world

import (
	"context"
	"errors"
	"fmt"

	"github.com/hollow-cube/hc-services/services/session-service/internal/db"
	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/server"
	"go.uber.org/fx"
)

var ErrNoServerAvailable = errors.New("no server available")

// Tracker is responsible for keeping track of all active map worlds on the server.
type Tracker struct {
	queries       *db.Queries
	serverTracker *server.Tracker
}

type TrackerParams struct {
	fx.In

	Queries       *db.Queries
	ServerTracker *server.Tracker
}

func NewTracker(p TrackerParams) *Tracker {
	return &Tracker{
		queries:       p.Queries,
		serverTracker: p.ServerTracker,
	}
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
