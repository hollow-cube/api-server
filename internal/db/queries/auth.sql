
-- name: GetActiveSession :one
select sess.player_id,
       client.kind             as client_kind,
       client.key_id           as client_key_id,
       sess.absolute_expires_at as absolute_expires_at
from api_sessions sess
       inner join api_clients client on sess.client_id = client.id
where sess.id = $1
  and sess.revoked_at is null
  and sess.idle_expires_at > now()
  and sess.absolute_expires_at > now();

-- name: CreateLaunchGrant :exec
insert into api_launch_grants (code_hash, player_id, map_id, expires_at)
values ($1, $2, $3, $4);

-- name: GetLaunchGrantForRedeem :one
select id, player_id, map_id
from api_launch_grants
where code_hash = $1
  and redeemed_at is null
  and expires_at > now()
for update;

-- name: UpsertApiClient :one
insert into api_clients (kind, public_key, key_id, label)
values ($1, $2, $3, $4)
on conflict (key_id) do update set last_seen_at = now()
returning id;

-- name: RevokeSessionsForClientAccount :exec
update api_sessions
set revoked_at = now()
where client_id = $1
  and player_id = $2
  and revoked_at is null;

-- name: CreateSession :one
insert into api_sessions (client_id, player_id, idle_expires_at, absolute_expires_at)
values ($1, $2, $3, $4)
returning id;

-- name: MarkLaunchGrantRedeemed :exec
update api_launch_grants
set redeemed_at         = now(),
    redeemed_session_id = $2
where id = $1;

-- name: TouchSession :exec
update api_sessions
set last_active_at  = now(),
    idle_expires_at = $2
where id = $1;
