-- name: GetRecapById :one
select recap.id, player.id as player_id, player.username, recap.year, recap.data
from player_recaps recap
  inner join player_data player on player.id = recap.player_id
where recap.id = $1;

-- name: GetRecapByPlayerId :one
select recap.id, player.id as player_id, player.username, recap.year, recap.data
from player_recaps recap
  inner join player_data player on player.id = recap.player_id
where player.id = $1 and recap.year = $2;
