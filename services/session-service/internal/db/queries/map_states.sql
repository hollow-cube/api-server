-- name: InsertMapState :one
insert into map_states(id, map_id, server, state)
values ($1, $2, $3, $4)
returning *;
