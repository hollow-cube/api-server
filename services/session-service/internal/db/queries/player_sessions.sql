-- name: ListPlayerSessions :many
select *
from player_sessions;

-- name: GetPlayerSession :one
select *
from player_sessions
where player_id = $1;

-- name: UpsertPlayerSession :one
insert into player_sessions(player_id, proxy_id, hidden, username, skin_texture, skin_signature, protocol_version,
                            p_type, p_state, p_instance_id, p_map_id, p_start_time)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
on conflict (player_id) do update
    set proxy_id         = excluded.proxy_id,
        hidden           = excluded.hidden,
        username         = excluded.username,
        skin_texture     = excluded.skin_texture,
        skin_signature   = excluded.skin_signature,
        protocol_version = excluded.protocol_version,
        p_type           = excluded.p_type,
        p_state          = excluded.p_state,
        p_instance_id    = excluded.p_instance_id,
        p_map_id         = excluded.p_map_id,
        p_start_time     = excluded.p_start_time
returning *;

-- name: ListTimedOutPlayers :many
select player_id
from player_sessions
where last_seen < now() - interval '30 seconds'
  and server_id != 'devserver';

-- name: UpdatePlayerLastSeenByServer :exec
update player_sessions
set last_seen = now()
where server_id = $1::text
  and player_id = any (@players::uuid[]);

-- name: DeletePlayerSession :one
delete
from player_sessions
where player_id = $1
returning *;
