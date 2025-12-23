package db

import (
	"context"

	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/model"
)

func (s *Store) AddExperience(ctx context.Context, iD string, experience int64) (int64, error) {
	result, err := s.addExperience(ctx, iD, experience)
	if err != nil {
		return 0, err
	}

	go s.metrics.Write(&model.ExpChanged{
		PlayerId: iD,
		Delta:    experience,
		NewValue: result,
	})

	return result, err
}
