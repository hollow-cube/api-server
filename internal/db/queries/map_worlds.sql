-- name: GetWorldsForMap :many
select *
from map_worlds
where map_id = $1;

-- name: InsertMapWorld :exec
insert into map_worlds (id, map_id, server_id, type)
values ($1, $2, $3, $4)
on conflict (id) do nothing;

-- name: DeleteMapWorld :exec
delete
from map_worlds
where id = $1;
