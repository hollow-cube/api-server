-- name: GetMapRatingForMapBy :one
select *
from map_ratings
where map_id = $1
  and player_id = $2;

-- name: UpsertMapRating :exec
insert into map_ratings (map_id, player_id, rating)
values ($1, $2, $3)
on conflict (map_id, player_id) do update
  set rating = excluded.rating;