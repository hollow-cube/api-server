begin;

create type map_tag as enum ('autocomplete', 'bossbattle', 'escape', 'exploration', 'interior', 'organics', 'puzzle', 'recreation', 'story', 'strategy', 'structure', 'terrain', 'trivia', 'twodimensional');

create table if not exists map_tags
(
  map_id uuid    not null references maps (id) on delete cascade,
  tag    map_tag not null,
  primary key (map_id, tag)
);

insert into map_tags (map_id, tag)
select distinct m.id, lower(unnest(m.opt_tags))::map_tag
from maps m
where m.opt_tags is not null
on conflict (map_id, tag) do nothing;

drop view if exists maps_published;
create or replace view maps_published as
select m.*,
       array(select tag from map_tags where map_id = m.id)::map_tag[] as tags,
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
from maps as m
  left join (select map_id, play_count, win_count, win_count::float / nullif(play_count, 0)::float as clear_rate
             from map_stats) stats on m.id = stats.map_id
  left join (select map_id, sum(case when rating = 1 then 1 when rating = 2 then -1 else 0 end) as total_likes
             from map_ratings
             group by map_id) likes on m.id = likes.map_id
where m.deleted_at is null
  and m.published_at is not null
  and m.published_id is not null;

commit;
