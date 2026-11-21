-- name: Unsafe_GetPlayerData :one
select *
from map_player_data
where id = $1;
