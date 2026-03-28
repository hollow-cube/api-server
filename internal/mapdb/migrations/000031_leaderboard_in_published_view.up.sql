begin;

drop view if exists maps_published;
create or replace view maps_published as
select m.*,
       array(select tag from map_tags where map_id = m.id order by index)::map_tag[] as tags,
       coalesce(stats.play_count, 0) as play_count,
       coalesce(stats.win_count, 0) as win_count,
       coalesce(stats.clear_rate::float, 0::float)::float as clear_rate,
       case
         when stats.play_count < 10 then -1
         when stats.clear_rate < 0.05 then 4
         when stats.clear_rate < 0.25 then 3
         when stats.clear_rate < 0.5 then 2
         when stats.clear_rate < 0.75 then 1
         else 0
         end as difficulty
from maps as m
  left join (select map_id, play_count, win_count, win_count::float / nullif(play_count, 0)::float as clear_rate
             from map_stats) stats on m.id = stats.map_id
where m.deleted_at is null
  and m.published_at is not null
  and m.published_id is not null;

commit;
