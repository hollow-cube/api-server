package v4Internal

import (
	"context"
	"errors"
	"fmt"

	"github.com/hollow-cube/api-server/internal/playerdb"
	"github.com/hollow-cube/api-server/pkg/ox"
)

type GetPlayerRecapRequest struct {
	PlayerID string `path:"playerId"`
	Year     int    `path:"year"`
}

// GET /recap/{playerId}/{year}
func (s *Server) GetPlayerRecap(ctx context.Context, request GetPlayerRecapRequest) (string, error) {
	recap, err := s.playerStore.GetRecapByPlayerId(ctx, request.PlayerID, request.Year)
	if errors.Is(err, playerdb.ErrNoRows) {
		return "", ox.NotFound{}
	} else if err != nil {
		return "", fmt.Errorf("failed to get player recap: %w", err)
	}

	return recap.ID, nil
}
