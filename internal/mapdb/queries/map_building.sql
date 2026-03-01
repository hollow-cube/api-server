-- name: GetRandomHeads :many
select *
from head_db
order by random()
limit $1;

-- name: GetHeadsWithSearch :many
select sqlc.embed(head_db),
       count(*) over () as total_count
from head_db
where name ilike $1::text
limit $2 offset $3;

-- name: GetHeadsWithCategory :many
select sqlc.embed(head_db),
       count(*) over () as total_count
from head_db
where category = $1::text
limit $2 offset $3;
