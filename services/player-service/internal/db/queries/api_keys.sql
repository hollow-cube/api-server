-- name: GetApiKeyByHash :one
select * from api_keys where deleted_at is null and key_hash = $1 limit 1;

-- name: InsertApiKey :exec
insert into api_keys (key_hash, player_id)
values ($1, $2);

-- name: DeleteAllApiKeys :exec
update api_keys
set deleted_at = now()
where deleted_at is null and player_id = $1;
