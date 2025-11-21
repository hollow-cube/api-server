package storage

import (
	"context"
	"errors"

	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/model"
	"github.com/jackc/pgx/v5"
)

func (c *PostgresClient) GetPlayerSession(ctx context.Context, playerId string) (data []byte, err error) {
	const query = `
		select state
		from tf_player_session
		where player_id = $1;
	`

	r := c.pool.QueryRow(ctx, query, playerId)
	if err = r.Scan(&data); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}

		return nil, err
	}

	return data, nil
}

func (c *PostgresClient) GetLocalSession(ctx context.Context, playerId string, worldId string) (data []byte, err error) {
	const query = `
		select state
		from tf_local_session
		where player_id = $1 and world_id = $2;
	`

	r := c.pool.QueryRow(ctx, query, playerId, worldId)
	if err = r.Scan(&data); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}

		return nil, err
	}

	return data, nil
}

func (c *PostgresClient) UpsertPlayerSession(ctx context.Context, playerId string, state []byte) error {
	const query = `
		insert into tf_player_session (player_id, state)
		values ($1, $2)
		on conflict (player_id) do update
		set state = excluded.state;
	`

	return c.safeExec(ctx, query, playerId, state)
}

func (c *PostgresClient) UpsertLocalSession(ctx context.Context, playerId string, worldId string, state []byte) error {
	const query = `
		insert into tf_local_session (player_id, world_id, state)
		values ($1, $2, $3)
		on conflict (player_id, world_id) do update
		set state = excluded.state;
	`

	return c.safeExec(ctx, query, playerId, worldId, state)
}

func (c *PostgresClient) GetAllSchematics(ctx context.Context, playerId string) ([]*model.SchematicHeader, error) {
	const query = `
		select name, dimensions, size, filetype
		from tf_schematics
		where player_id = $1;
	`

	rows, err := c.pool.Query(ctx, query, playerId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var headers []*model.SchematicHeader
	for rows.Next() {
		var schem model.SchematicHeader
		if err = rows.Scan(&schem.Name, &schem.Dimensions, &schem.Size, &schem.FileType); err != nil {
			return nil, err
		}
		headers = append(headers, &schem)
	}

	return headers, nil
}

func (c *PostgresClient) GetSchematicData(ctx context.Context, playerId, schemName string) ([]byte, error) {
	const query = `
		select schem_data
		from tf_schematics
		where player_id = $1 and name = $2;
	`

	r := c.pool.QueryRow(ctx, query, playerId, schemName)
	var data []byte
	if err := r.Scan(&data); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}

		return nil, err
	}

	return data, nil
}

func (c *PostgresClient) CreateSchematic(ctx context.Context, playerId string, header *model.SchematicHeader, data []byte) error {
	const query = `
		insert into tf_schematics (player_id, name, dimensions, size, filetype, schem_data)
		values ($1, $2, $3, $4, $5, $6);
	`

	return c.safeExec(ctx, query, playerId, header.Name, header.Dimensions, header.Size, header.FileType, data)
}

func (c *PostgresClient) UpdateSchematicHeader(ctx context.Context, playerId string, header *model.SchematicHeader) error {
	const query = `
		update tf_schematics
		set dimensions = $3, filetype = $4
		where player_id = $1 and name = $2;
	`

	result, err := c.safeExecWithResult(ctx, query, playerId, header.Name, header.Dimensions, header.FileType)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil

}

func (c *PostgresClient) DeleteSchematic(ctx context.Context, playerId, schemName string) error {
	const query = `
		delete from tf_schematics
		where player_id = $1 and name = $2;
	`

	result, err := c.safeExecWithResult(ctx, query, playerId, schemName)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
