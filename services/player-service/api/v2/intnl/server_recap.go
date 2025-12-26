package intnl

import (
	"context"
	"errors"
	"fmt"

	"github.com/hollow-cube/hc-services/services/player-service/internal/db"
)

func (s *server) GetPlayerRecap(ctx context.Context, request GetPlayerRecapRequestObject) (GetPlayerRecapResponseObject, error) {
	recap, err := s.store.GetRecapByPlayerId(ctx, request.PlayerId, request.Year)
	if errors.Is(err, db.ErrNoRows) {
		return GetPlayerRecap404Response{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get player recap: %w", err)
	}

	return GetPlayerRecap200TextResponse(recap.ID), nil
}
