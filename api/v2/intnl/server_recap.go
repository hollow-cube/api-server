package intnl

import (
	"context"
	"errors"
	"fmt"

	"github.com/hollow-cube/api-server/internal/playerdb"
)

func (s *Server) GetPlayerRecap(ctx context.Context, request GetPlayerRecapRequestObject) (GetPlayerRecapResponseObject, error) {
	recap, err := s.store.GetRecapByPlayerId(ctx, request.PlayerId, request.Year)
	if errors.Is(err, playerdb.ErrNoRows) {
		return GetPlayerRecap404Response{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to get player recap: %w", err)
	}

	return GetPlayerRecap200TextResponse(recap.ID), nil
}
