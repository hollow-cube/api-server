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

-- name: GetAllBestCompletedSaveStatesForMap :many
select distinct on (player_id) ss.*
from save_states ss
         join maps m on m.id = ss.map_id
where ss.deleted is null
  and ss.map_id = $1
  and (ss.type = 'playing' or ss.type = 'verifying')
  and ss.completed = true
order by player_id, case
    when m.leaderboard is not null and (m.leaderboard ->> 'asc')::boolean = false
        then -coalesce(ss.score, greatest(ss.playtime, ss.ticks * 50))
    else coalesce(ss.score, greatest(ss.playtime, ss.ticks * 50))
end;

-- name: GetLatestSaveState :one
select *
from save_states
where deleted is null
  and map_id = $1
  and player_id = $2
  and type = $3
order by updated desc
limit 1;

-- name: GetBestSaveState :one
select ss.*
from save_states ss
  join maps m on m.id = ss.map_id
where ss.deleted is null
  and ss.map_id = $1
  and ss.player_id = $2
  and (ss.type = 'playing' or ss.type = 'verifying')
  and ss.completed = true
order by case
           when m.leaderboard is not null
             and (m.leaderboard ->> 'asc')::boolean = false
             then -coalesce(ss.score, greatest(ss.playtime, ss.ticks * 50))
           else coalesce(ss.score, greatest(ss.playtime, ss.ticks * 50))
           end
limit 1;

-- name: GetBestSaveStateSinceBeta :one
select *
from save_states
where deleted is null
  and map_id = $1
  and player_id = $2
  and type = 'playing'
  and completed = true
  and created > '2024-04-05T09:00:00-04:00'::timestamptz
order by coalesce(score, greatest(playtime, ticks * 50))
limit 1;

-- name: CreateSaveState :one
insert into save_states (id, map_id, player_id, type, created, updated, completed, playtime, data_version,
                         state_v2, protocol_version)
values (gen_random_uuid(), $1, $2, $3, now(), now(), false, 0, 0, 'null', $4)
returning *;

-- name: UpsertSaveState :exec
insert into public.save_states
(id, map_id, player_id, type, created, updated, completed, playtime, ticks, score, state_v2, data_version,
 protocol_version)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
on conflict (id, map_id, player_id) do update
  set updated          = excluded.updated,
      completed        = excluded.completed,
      playtime         = excluded.playtime,
      ticks            = excluded.ticks,
      score            = excluded.score,
      state_v2         = excluded.state_v2,
      data_version     = excluded.data_version,
      protocol_version = excluded.protocol_version;

-- name: DeleteSaveState :execrows
delete
from save_states
where id = $1
  and map_id = $2
  and player_id = $3;

-- name: DeleteVerifyingStates :exec
update save_states
set deleted = now()
where map_id = $1
  and type = 'verifying'
  and deleted is null;

-- name: DeleteMapPlayerSaveStates :exec
update public.save_states
set deleted = now()
where deleted is null
  and map_id = $1
  and player_id = $2;

-- name: Unsafe_DeleteMapSaveStates :exec
update save_states
set deleted = now()
where map_id = $1
  and deleted is null;

-- name: GetRecentMaps :many
select map_id
from save_states
where player_id = @player_id
  and type = @type
  and deleted is null
group by map_id
order by max(updated) desc
offset sqlc.arg('page')::int * sqlc.arg('page_size')::int limit sqlc.arg('page_size')::int + 1;

-- name: GetCompletedMaps :many
select distinct map_id
from save_states
where deleted is null
  and completed = true
  and player_id = $1;
