-- name: CreatePlayerData :one
insert into player_data (id, username, first_join, last_online, skin)
values ($1, $2, now(), now(), $3)
returning *;

-- name: GetPlayerData :one
select *
from player_data
where id = $1
limit 1;

-- name: PlayerExistsById :one
select exists (select 1
               from public.player_data
               where id = $1);

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

-- name: UpdatePlayerData :exec
update public.player_data
set username     = coalesce(sqlc.narg('username'), username),
    last_online  = coalesce(sqlc.narg('last_online'), last_online),
    playtime     = coalesce(sqlc.narg('playtime'), playtime),
    beta_enabled = coalesce(sqlc.narg('beta_enabled'), beta_enabled),
    settings     = coalesce(sqlc.narg('settings'), settings),
    skin         = sqlc.narg('skin')
where id = $1;

-- name: addExperience :one
update public.player_data
set experience = experience + $2
where id = $1
  and experience + $2 >= 0
returning experience as exp;
-- SQLC is a bit dumb and redeclares the 'experience' variable so we have to rename it

-- name: SearchPlayersFuzzy :many
select id, username
from player_data
where username ~* $1
limit 25;

-- name: GetTOTP :one
select *
from player_totp
where player_id = $1;

-- name: AddTOTP :execrows
insert into player_totp (player_id, active, key, recovery_codes)
values ($1, $2, $3, $4)
on conflict (player_id)
  do update set key            = excluded.key,
                recovery_codes = excluded.recovery_codes,
                created_at     = now()
where player_totp.active = false;

-- name: ActivateTOTP :one
update player_totp
set active = true
where player_id = $1
  and key = $2
  and active = false
returning 1;

-- name: DeleteTOTP :exec
delete
from player_totp
where player_id = $1;
