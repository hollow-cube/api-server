package intnl

import (
	"context"
	"errors"
	"fmt"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/storage"
)

func (s *server) GetPlayerRecap(ctx context.Context, request GetPlayerRecapRequestObject) (GetPlayerRecapResponseObject, error) {
	recap, err := s.storageClient.GetPlayerRecapByPlayer(ctx, request.PlayerId, request.Year)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return GetPlayerRecap404Response{}, nil
		}
		return nil, fmt.Errorf("failed to get player recap: %w", err)
	}
	return GetPlayerRecap200TextResponse(recap.Id), nil
}
