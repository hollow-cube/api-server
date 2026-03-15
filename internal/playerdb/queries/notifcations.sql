-- name: GetNotifications :many
select sqlc.embed(player_notifications),
       count(*) over () as total_count
from player_notifications
where player_id = $1
  and deleted_at is null
  and (expires_at is null or expires_at > now())
  and (not $2 or read_at is null)
order by created_at desc
limit $3 offset $4;

-- name: MarkNotificationRead :execrows
update player_notifications
set read_at = now()
where id = $1;

-- name: MarkNotificationUnread :execrows
update player_notifications
set read_at = null
where id = $1;

-- name: DeleteNotification :execrows
update player_notifications
set deleted_at = now()
where id = $1
  and deleted_at is null;

-- name: DeleteMatching :many
update player_notifications
set deleted_at = now()
where deleted_at is null
  and (sqlc.narg('player_id')::uuid is null or player_id = sqlc.narg('player_id')::uuid)
  and (sqlc.narg('key')::text is null or key = sqlc.narg('key')::text)
returning *;

-- name: Unsafe_DeleteNotification :exec
delete
from player_notifications
where type = $1
  and key = $2
  and player_id = $3;

-- name: Unsafe_AddNotification :exec
insert into player_notifications (id, type, key, player_id, data, expires_at)
values (gen_random_uuid(), $1, $2, $3, $4, $5);