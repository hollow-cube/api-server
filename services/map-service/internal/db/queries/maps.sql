-- name: CountMaps :one
select count(*)
from maps
where deleted_at is null
  and published_at is not null;

-- name: GetMapsById :many
select sqlc.embed(m),
       coalesce(stats.play_count, 0) as play_count,
       coalesce(stats.win_count, 0) as win_count,
       coalesce(likes.total_likes, 0) as total_likes
from public.maps as m
  left join (select map_id, play_count, win_count
             from map_stats
             group by map_id) stats on m.id = stats.map_id
  left join (select map_id, sum(case when rating = 1 then 1 when rating = 2 then -1 else 0 end) as total_likes
             from map_ratings
             group by map_id) likes on m.id = likes.map_id
where m.deleted_at is null
  and id = any ($1);

-- name: UpdateMapStats :exec
insert into map_stats (map_id, play_count, win_count)
select $1 as map_id,
       count(distinct player_id) as play_count,
       count(distinct case when completed then player_id end) as win_count
from save_states
where map_id = $1
on conflict (map_id) do update
  set play_count=excluded.play_count,
      win_count=excluded.win_count;
