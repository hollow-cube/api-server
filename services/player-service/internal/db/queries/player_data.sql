-- name: GetPlayerData :one
select *
from player_data
where id = $1
limit 1;
