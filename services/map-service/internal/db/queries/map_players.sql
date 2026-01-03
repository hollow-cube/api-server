-- name: Unsafe_GetPlayerData :one
select *
from map_player_data
where id = $1;

-- name: UpsertPlayerData :exec
insert into map_player_data (id, unlocked_slots, last_played_map, last_edited_map, contest_slot)
values ($1, $2, $3, $4, $5)
on conflict (id) do update
  set last_played_map=excluded.last_played_map,
      last_edited_map=excluded.last_edited_map,
      contest_slot=excluded.contest_slot;

-- name: GetIndexedMapSlots :many
select map_id, index
from map_slots
where player_id = $1
  and index >= 0
  and index < 5;

-- name: GetMapSlots :many
select *
from map_slots
where player_id = $1;

-- name: InsertMapSlot :exec
insert into map_slots(player_id, map_id, index, created_at)
values ($1, $2, $3, $4);

-- name: RemoveMapFromSlots :many
delete
from map_slots
where map_id = $1
returning player_id;
