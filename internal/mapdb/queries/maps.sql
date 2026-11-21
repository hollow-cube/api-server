-- name: GetMapById :one
select *
from maps
where deleted_at is null
  and id = $1;

-- name: GetMapIsolateConfig :one
select size, opt_extra
from maps
where deleted_at is null
  and id = $1;

-- name: GetPublishedMapById :one
select sqlc.embed(maps_published),
       array(select tag from map_tags where map_id = maps_published.id order by index)::map_tag[] as tags
from maps_published
where id = $1;
