-- name: GetRandomHeads :many
select *
from head_db
order by random()
limit $1;

-- name: GetHeadCountWithSearch :one
select count(*) as head_count
from head_db
where name ilike $1::text;

-- name: GetHeadsWithSearch :many
select *
from head_db
where name ilike $1::text
limit $2 offset $3;

-- name: GetHeadCountWithCategory :one
select count(*) as head_count
from head_db
where category = $1::text;

-- name: GetHeadsWithCategory :many
select *
from head_db
where category = $1::text
limit $2 offset $3;
