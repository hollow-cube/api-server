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
