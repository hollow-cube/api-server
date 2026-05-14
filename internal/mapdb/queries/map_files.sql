-- name: GetProjectFiles :many
select path,
       content_type,
       content_hash,
       size
from map_files
where map_id = $1;

-- name: GetProjectFile :one
select path, content, content_type, size
from map_files
where map_id = $1
  and path = $2;

-- name: UpsertProjectFile :one
insert into map_files (map_id, path, content, content_hash, content_type)
values ($1, $2, $3, $4, $5)
on conflict (map_id, path) do update
  set content      = excluded.content,
      content_hash = excluded.content_hash,
      content_type = excluded.content_type,
      updated_at   = now()
returning path, content_type, content_hash, size;

-- name: DeleteProjectFile :one
delete
from map_files
where map_id = $1
  and path = $2
returning path as deleted_path;
