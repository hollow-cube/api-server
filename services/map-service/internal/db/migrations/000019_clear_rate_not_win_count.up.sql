drop view if exists maps_published;

create or replace view maps_published as
select m.id,
       m.owner,
       m.m_type,
       m.created_at,
       m.updated_at,
       m.authz_key,
       m.verification,
       m.file_id,
       m.legacy_map_id,
       m.published_id,
       m.published_at,
       m.quality_override,
       m.opt_name,
       m.opt_icon,
       m.size,
       m.opt_variant,
       m.opt_subvariant,
       m.opt_spawn_point,
       m.opt_only_sprint,
       m.opt_no_sprint,
       m.opt_no_jump,
       m.opt_no_sneak,
       m.opt_boat,
       m.opt_extra,
       m.opt_tags,
       m.protocol_version,
       m.contest,
       m.listed,
       m.ext,
       coalesce(stats.play_count, 0) as play_count,
       coalesce(stats.win_count, 0) as win_count,
       coalesce(likes.total_likes, 0) as total_likes,
       coalesce(stats.clear_rate::float, 0::float)::float as clear_rate,
       case
         when stats.play_count < 10 then -1
         when stats.clear_rate < 0.05 then 4
         when stats.clear_rate < 0.25 then 3
         when stats.clear_rate < 0.5 then 2
         when stats.clear_rate < 0.75 then 1
         else 0
         end as difficulty
from public.maps as m
  left join (select map_id, play_count, win_count, win_count::float / nullif(play_count, 0)::float as clear_rate
             from map_stats) stats on m.id = stats.map_id
  left join (select map_id, sum(case when rating = 1 then 1 when rating = 2 then -1 else 0 end) as total_likes
             from map_ratings
             group by map_id) likes on m.id = likes.map_id
where m.deleted_at is null
  and m.published_at is not null
  and m.published_id is not null;
