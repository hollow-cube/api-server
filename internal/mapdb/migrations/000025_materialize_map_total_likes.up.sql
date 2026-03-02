begin;

alter table maps
    add column total_likes int not null default 0;

create or replace function update_map_likes_count()
    returns trigger as
$$
begin
    if tg_op = 'INSERT' then
        if new.rating = 1 then
            update maps set total_likes = total_likes + 1 where id = new.map_id;
        elseif new.rating = 2 then
            update maps set total_likes = total_likes - 1 where id = new.map_id;
        end if;

    elseif tg_op = 'DELETE' then
        if old.rating = 1 then
            update maps set total_likes = total_likes - 1 where id = old.map_id;
        elseif old.rating = 2 then
            update maps set total_likes = total_likes + 1 where id = old.map_id;
        end if;

    elseif tg_op = 'UPDATE' then
        update maps
        set total_likes = total_likes +
                          case when new.rating = 1 then 1 when new.rating = 2 then -1 else 0 end -
                          case when old.rating = 1 then 1 when old.rating = 2 then -1 else 0 end
        where id = new.map_id; -- Could technically break if map_id is changed... but please for the love of god we're never doing that
    end if;

    return null;
end;
$$ language plpgsql;

create trigger map_ratings_update_likes_count
    after insert or delete or update of rating
    on map_ratings
    for each row
execute function update_map_likes_count();

update maps m
set total_likes = (select coalesce(sum(
                                           case
                                               when rating = 1 then 1
                                               when rating = 2 then -1
                                               else 0
                                               end
                                   ), 0)
                   from map_ratings mr
                   where mr.map_id = m.id);

-- We must drop the view before re-creating it as field order has changed. sqlc will be fine with this, it's just Postgres.
drop view if exists maps_published;

-- Just remove the total_likes as it is now included in m.*
create or replace view maps_published as
select m.*,
       array(select tag from map_tags where map_id = m.id)::map_tag[] as tags,
       coalesce(stats.play_count, 0)                                  as play_count,
       coalesce(stats.win_count, 0)                                   as win_count,
       coalesce(stats.clear_rate::float, 0::float)::float             as clear_rate,
       case
           when stats.play_count < 10 then -1
           when stats.clear_rate < 0.05 then 4
           when stats.clear_rate < 0.25 then 3
           when stats.clear_rate < 0.5 then 2
           when stats.clear_rate < 0.75 then 1
           else 0
           end                                                        as difficulty
from maps as m
         left join (select map_id, play_count, win_count, win_count::float / nullif(play_count, 0)::float as clear_rate
                    from map_stats) stats on m.id = stats.map_id
where m.deleted_at is null
  and m.published_at is not null
  and m.published_id is not null;

commit;

