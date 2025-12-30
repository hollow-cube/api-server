-- name: TfGetPlayerSession :one
select state
from tf_player_session
where player_id = $1;

-- name: TfUpsertPlayerSession :exec
insert into tf_player_session (player_id, state)
values ($1, $2)
on conflict (player_id) do update
  set state = excluded.state;

-- name: TfGetLocalSession :one
select state
from tf_local_session
where player_id = $1
  and world_id = $2;

-- name: TfUpsertLocalSession :exec
insert into tf_local_session (player_id, world_id, state)
values ($1, $2, $3)
on conflict (player_id, world_id) do update
  set state = excluded.state;

-- name: TfGetAllSchematics :many
select name, dimensions, size, filetype
from tf_schematics
where player_id = $1;

-- name: TfGetSchematicData :one
select schem_data
from tf_schematics
where player_id = $1
  and name = $2;

-- name: TfCreateSchematic :exec
insert into tf_schematics (player_id, name, dimensions, size, filetype, schem_data)
values ($1, $2, $3, $4, $5, $6);

-- name: TfUpdateSchematicHeader :exec
update tf_schematics
set dimensions = $3,
    filetype   = $4
where player_id = $1
  and name = $2;

-- name: TfDeleteSchematic :exec
delete
from tf_schematics
where player_id = $1
  and name = $2;
