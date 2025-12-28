-- name: Unsafe_GetPlayerData :one
select *
from map_player_data
where id = $1;

-- name: UpsertPlayerData :exec
insert into map_player_data (id, unlocked_slots, maps, last_played_map, last_edited_map, contest_slot)
values ($1, $2, $3, $4, $5, $6)
on conflict (id) do update
  set maps=excluded.maps,
      last_played_map=excluded.last_played_map,
      last_edited_map=excluded.last_edited_map,
      contest_slot=excluded.contest_slot;

-- name: RemoveMapFromSlots :many
update map_player_data
set maps = array_replace(maps, $1, '')
where $1 = any (maps)
returning *;