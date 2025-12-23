-- name: CreatePlayerData :one
insert into public.player_data (id, username, first_join, last_online)
values ($1, $2, $3, $4)
RETURNING *;

-- name: GetPlayerData :one
select *
from public.player_data
where id = $1
limit 1;

-- name: PlayerExistsById :one
SELECT exists (SELECT 1
               FROM public.player_data
               WHERE id = $1);

-- name: LookupPlayerByUsername :one
select id
from public.player_data
where lower(username) = lower($1);

-- name: LookupPlayerByIdOrUsername :one
select id
from public.player_data
where id = $1
   or lower(username) = lower($2);

-- name: GetPlayerStats :one
select count(*), sum(playtime)
from public.player_data;