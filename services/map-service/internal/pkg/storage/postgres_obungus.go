package storage

import (
	"context"

	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/model"
)

func (c *PostgresClient) GetUnreviewedBox(ctx context.Context, player string) (*model.Box, error) {
	const query = `
		select id, player_id, created_at, name, shape::text, schematic_data, legacy_username
		from obungus_pending_boxes b
		where not exists (
			select 1
			from obungus_box_ratings r
			where r.box_id = b.id
			  and r.player_id = $1
		)
		order by random()
		limit 1;
	`

	var box model.Box
	err := c.pool.QueryRow(ctx, query, player).Scan(
		&box.Id, &box.PlayerId, &box.CreatedAt,
		&box.Name, &box.Shape, &box.SchematicData,
		&box.LegacyUsername,
	)
	if err != nil {
		return nil, err
	}

	return &box, nil
}
