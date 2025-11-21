-- name: ListServerIDs :many
select id
from server_states;

-- name: ListServersWithRoleExcept :many
select id
from server_states
where role = $1
  and status = 1
  and id != @excluding::text;

-- name: GetServerState :one
select *
from server_states
where id = $1;

-- name: GetFirstServerStateByMap :one
select server_states.*
from server_states
         inner join public.map_states ms on server_states.id = ms.server
where map_id = $1
  and status_v2 = 'active';

-- name: InsertServerState :one
insert into server_states(id, role, status, cluster_ip)
values ($1, $2, $3, $4)
returning *;

-- name: UpdateServerState :exec
update server_states
set status       = $2,
    cluster_ip   = $3,
    status_v2    = $4,
    status_since = $5
where id = $1;

-- name: UpdateServerStatus :exec
update server_states
set status_v2    = $2,
    status_since = now()
where id = $1;

-- name: DeleteServerState :exec
delete
from server_states
where id = $1;
