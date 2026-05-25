package v1Public

import (
	"context"
	"errors"

	"github.com/hollow-cube/api-server/api/auth"
	"github.com/hollow-cube/api-server/internal/mapdb"
	"github.com/hollow-cube/api-server/pkg/ox"
)

type MapRequest struct {
	MapID string `path:"mapId"`
}

func (s *Server) mapForAuthPlayer(ctx context.Context, mapID string) (*mapdb.Map, error) {
	playerID, ok := auth.GetPlayerID(ctx)
	if !ok {
		return nil, ox.Unauthorized{}
	}

	m, err := s.mapStore.GetMapById(ctx, mapID)
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
