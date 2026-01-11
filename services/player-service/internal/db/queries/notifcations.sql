-- name: GetNotifications :many
select id, type, key, data, created_at, read_at, expires_at
from player_notifications
where player_id = $1
  and deleted_at is null
  and (expires_at is null or expires_at > now())
  and (not $4 or read_at is null)
order by created_at desc
limit $2 offset $3;

-- name: GetNotificationCount :one
select count(*)
from player_notifications
where player_id = $1
  and deleted_at is null
  and (expires_at is null or expires_at > now())
  and (not $2 or read_at is null);

-- name: MarkNotificationRead :execrows
update player_notifications
set read_at = now()
where player_id = $1
  and id = $2;

-- name: MarkNotificationUnread :execrows
update player_notifications
set read_at = null
where player_id = $1
  and id = $2;

-- name: DeleteNotification :many
update player_notifications
set deleted_at = now()
where player_id = $1
  and id = $2
  and deleted_at is null
returning id;

-- name: DeleteNotifications :many
update player_notifications
set deleted_at = now()
where player_id = $1
  and type = $2
  and key = $3
  and deleted_at is null
returning id;

-- name: Unsafe_DeleteNotification :exec
delete
from player_notifications
where type = $1
  and key = $2
  and player_id = $3;

-- name: Unsafe_AddNotification :exec
insert into player_notifications (id, type, key, player_id, data, expires_at)
values (gen_random_uuid(), $1, $2, $3, $4, $5);