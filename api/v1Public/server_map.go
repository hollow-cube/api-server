package v1Public

import (
	"context"
	"errors"

	"github.com/hollow-cube/api-server/api/auth"
	"github.com/hollow-cube/api-server/internal/mapdb"
	"github.com/hollow-cube/api-server/pkg/ox"
	"go.uber.org/zap"
)

type MapRequest struct {
	MapID string `path:"mapId"`
}

func (s *Server) mapForAuthPlayer(ctx context.Context, mapID string) (*mapdb.Map, error) {
	playerID, ok := auth.GetPlayerID(ctx)
	zap.S().Infow("mapForAuthPlayer", "player id", playerID, "ok", ok, "map id", mapID)
	if !ok {
		return nil, ox.Unauthorized{}
	}

	m, err := s.mapStore.GetMapById(ctx, mapID)
	zap.S().Infow("mapForAuthPlayer: got map", "map", m, "err", err)
	if errors.Is(err, mapdb.ErrNoRows) {
		return nil, ox.NotFound{}
	}

	if err != nil {
		return nil, err
	}

	// TODO: handle added builders
	// TODO: handle staff override
	if m.Owner != playerID {
		return nil, ox.Forbidden{}
	}

	return &m, nil
}
