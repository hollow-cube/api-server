-- name: CountFailSaveStates :one
select count(*)
from save_states
where type = 'playing'
  and completed = false;

-- name: GetSaveState :one
select *
from public.save_states
where deleted is null
  and id = $1
  and map_id = $2
  and player_id = $3;

-- name: CreateSaveState :one
insert into save_states (id, map_id, player_id, type, created, updated, completed, playtime, data_version,
                         state_v2, protocol_version)
values (gen_random_uuid(), $1, $2, $3, now(), now(), false, 0, 0, 'null', $4)
returning *;

-- name: UpsertSaveState :exec
insert into public.save_states
(id, map_id, player_id, type, created, updated, completed, playtime, ticks, state_v2, data_version, protocol_version)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
on conflict (id, map_id, player_id) do update
  set updated          = excluded.updated,
      completed        = excluded.completed,
      playtime         = excluded.playtime,
      ticks            = excluded.ticks,
      state_v2         = excluded.state_v2,
      data_version     = excluded.data_version,
      protocol_version = excluded.protocol_version;

-- name: DeleteVerifyingStates :exec
delete
from save_states
where map_id = $1
  and type = 'verifying';

-- name: Unsafe_DeleteMapSaveStates :exec
update save_states
set deleted = now()
where map_id = $1;
