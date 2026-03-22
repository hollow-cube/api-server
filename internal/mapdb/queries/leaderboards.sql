-- name: GetMapsBeatenLeaderboard :many
select s1.player_id,
       count(distinct s1.map_id) as unique_maps_beaten
from save_states as s1
         join maps on s1.map_id = maps.id
where s1.deleted is null
  and s1.completed = true
  and (s1.type = 'playing' or s1.type = 'verifying')
  and maps.published_at is not null
group by s1.player_id
order by unique_maps_beaten desc
limit 10;

-- name: GetMapsBeatenLeaderboardForPlayer :one
select count(distinct map_id) as unique_maps_beaten
from save_states
         join maps on save_states.map_id = maps.id
where deleted is null
  and completed = true
  and (type = 'playing' or type = 'verifying')
  and player_id = $1
  and maps.published_at is not null;

-- name: GetTopTimesLeaderboard :many
select s1.player_id,
       count(distinct s1.map_id) as top_times
from (select map_id, (round(min(playtime) / 50.0) * 50)::bigint as min_playtime
      from save_states
               join maps on save_states.map_id = maps.id
      where deleted is null
        and completed = true
        and playtime != 0
        and (type = 'playing' or type = 'verifying')
        and maps.published_at is not null
        and maps.deleted_at is null
        and coalesce(maps.leaderboard->>'format', 'time') = 'time'
      group by map_id) as shortest_playtimes
         join save_states as s1
              on s1.map_id = shortest_playtimes.map_id
                  and (round(s1.playtime / 50.0) * 50)::bigint = shortest_playtimes.min_playtime
where s1.deleted is null
  and s1.completed = true
group by s1.player_id
order by top_times desc
limit 10;

-- name: GetTopTimesLeaderboardForPlayer :one
select count(distinct s1.map_id) as top_times
from (select map_id, (round(min(playtime) / 50.0) * 50)::bigint as min_playtime
      from save_states
               join maps on save_states.map_id = maps.id
      where deleted is null
        and completed = true
        and playtime != 0
        and (type = 'playing' or type = 'verifying')
        and maps.published_at is not null
        and maps.deleted_at is null
        and coalesce(maps.leaderboard->>'format', 'time') = 'time'
      group by map_id) as shortest_playtimes
         join save_states as s1
              on s1.map_id = shortest_playtimes.map_id
                  and (round(s1.playtime / 50.0) * 50)::bigint = shortest_playtimes.min_playtime
                  and s1.player_id = $1
where s1.deleted is null
  and s1.completed = true;

-- name: GetPlayerBestTimes :many
-- Returns the player's best time per completed map
select distinct on (save_states.map_id) save_states.map_id,
                                        save_states.playtime,
                                        maps.opt_name as map_name,
                                        maps.published_id
from save_states
         join maps on save_states.map_id = maps.id
where save_states.deleted is null
  and save_states.completed = true
  and save_states.player_id = $1
  and (save_states.type = 'playing' or save_states.type = 'verifying')
  and save_states.playtime != 0
  and maps.published_at is not null
  and maps.deleted_at is null
order by save_states.map_id, save_states.playtime asc;