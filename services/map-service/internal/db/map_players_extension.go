package db

import (
	"context"
	"errors"
)

// GetPlayerData returns the player data for the given id or a default value, never ErrNoRows.
func (s *Store) GetPlayerData(ctx context.Context, playerId string) (MapPlayerData, error) {
	pd, err := s.Unsafe_GetPlayerData(ctx, playerId)
	if errors.Is(err, ErrNoRows) {
		pd.ID = playerId
		pd.Map = make([]string, 2)
	} else if err != nil {
		return MapPlayerData{}, err
	}

	return pd, nil
}
