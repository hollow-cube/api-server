-- name: GetPlayerData :one
select *
from player_data
where id = $1 limit 1;

-- name: LookupPlayerById :one
select id
from public.player_data
where id = $1;

-- name: LookupPlayerByUsername :one
select id
from public.player_data
where lower(username) = lower($1);

-- name: LookupPlayerByIdOrUsername :one
select id
from public.player_data
where id = $1
   or lower(username) = lower($2);