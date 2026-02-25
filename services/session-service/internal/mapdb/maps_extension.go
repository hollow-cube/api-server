package mapdb

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
)

func (s *Store) DeleteMap(ctx context.Context, id string, deletedBy, deletedReason *string) error {
	return TxNoReturn(ctx, s, func(ctx context.Context, tx *Store) (err error) {
		if err = tx.Unsafe_DeleteMapSaveStates(ctx, id); err != nil {
			return fmt.Errorf("failed to delete save states: %w", err)
		}

		if err = tx.Unsafe_DeleteMap(ctx, id, deletedBy, deletedReason); err != nil {
			return fmt.Errorf("failed to delete map: %w", err)
		}

		return nil
	})
}

func (s *Store) FindNextPublishedId(ctx context.Context) (id int, err error) {
	const maxPublishedMapId = 999_999_999

	for i := 0; i < 10; i++ {
		id = int(rand.Int63n(maxPublishedMapId-1) + 1) // We do not want zero as an Id

		_, err = s.GetPublishedMapByPublishedId(ctx, &id)
		if errors.Is(err, ErrNoRows) {
			// Found a free Id, return it.
			return id, nil
		} else if err != nil {
			return 0, fmt.Errorf("failed to check published id: %w", err)
		}
	}

	return 0, fmt.Errorf("failed to create new published id in 10 attempts")
}
