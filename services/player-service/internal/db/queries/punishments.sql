-- name: GetActivePunishment :one
select id, player_id, executor_id, type, created_at, ladder_id, comment,
       expires_at, revoked_by, revoked_at, revoked_reason
from punishments
where type = $1
  and player_id = $2
  and (expires_at is null or expires_at > now())
  and revoked_by is null
order by created_at desc
limit 1;

-- name: SearchPunishments :many
select *
from punishments
where (type = $1 or $1 = '')
  and player_id = $2
  and (executor_id = $3 or $3 = '')
  and (ladder_id = $4 or $4 = '' or $4 is null);

-- name: CreatePunishment :one
insert into punishments (player_id, executor_id, type, created_at, ladder_id, comment, expires_at)
values ($1, $2, $3, now(), $4, $5, $6)
returning *;

-- name: RevokePunishment :one
update punishments
set revoked_by     = $3,
    revoked_at     = now(),
    revoked_reason = $4
where type = $1
  and player_id = $2
  and (expires_at is null or expires_at > now())
  and revoked_by is null
returning *;