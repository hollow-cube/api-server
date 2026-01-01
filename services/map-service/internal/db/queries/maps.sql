-- name: CountMaps :one
select count(*)
from maps
where deleted_at is null
  and published_at is not null;

-- name: GetMapById :one
select *
from maps
where id = $1;

-- name: GetPublishedMapById :one
select *
from maps_published
where id = $1;

-- name: GetPublishedMapByPublishedId :one
select *
from maps_published
where published_id = $1;

-- name: MultiGetPublishedMapsById :many
select *
from maps_published
where id = any ($1::uuid[]);

-- name: CreateMap :one
insert into maps (id, owner, m_type, created_at, updated_at, authz_key, file_id, legacy_map_id, published_id,
                  published_at, opt_name, opt_icon,
                  opt_variant, opt_subvariant, opt_spawn_point, opt_extra, opt_tags, deleted_at, deleted_by,
                  deleted_reason, contest, size, protocol_version)
values ($1, $2, $3, now(), now(), '', '', '', null, null, coalesce($4, ''), coalesce($5, ''),
        $6, $7, $8, null, null, null, null, null, $9, $10, 769)
returning *;

-- name: UpdateMap :exec
update maps
set opt_name         = @name,
    opt_icon         = @icon,
    size             = @size,
    opt_variant      = @variant,
    opt_subvariant   = @subvariant,
    opt_spawn_point  = @spawn_point,
    opt_tags         = @tags,
    ext              = @ext,
    quality_override = @quality,
    listed           = @listed,
    protocol_version = @protocol_version,
    opt_only_sprint  = @only_sprint,
    opt_no_sprint    = @no_sprint,
    opt_no_jump      = @no_jump,
    opt_no_sneak     = @no_sneak,
    opt_boat         = @boat,
    opt_extra        = @extra
where id = $1;

-- name: UpdateMapVerification :exec
update maps
set verification     = $2,
    protocol_version = coalesce($3, protocol_version)
where id = $1;

-- name: PublishMap :exec
update maps
set updated_at   = now(),
    published_at = now(),
    published_id = $2,
    contest      = $3
where id = $1;

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

-- name: Unsafe_DeleteMap :exec
update maps
set deleted_at     = now(),
    deleted_by     = $2,
    deleted_reason = $3
where id = $1;

-- name: GetMultiMapProgress :many
with ranked_save_states as (select m.id::text as map_id,
                                   (case when ss.completed then 1 else 0 end)::int8 as completed,
                                   ss.playtime::int8 as playtime,
                                   ss.updated
                            from (select unnest($2::uuid[]) as id) m
                              left join
                            save_states ss
                            on
                              ss.map_id = m.id and ss.player_id = $1
                            where ss.deleted is null
                              and (ss.type = 'playing' or ss.type = 'verifying')),
     progress_and_playtime as (select map_id,
                                      coalesce(max(completed), 0) as progress,
                                      case
                                        when max(completed) = 1 then min(playtime) filter (where completed = 1)
                                        else (select playtime
                                              from ranked_save_states rss
                                              where rss.map_id = rs.map_id
                                              order by updated desc
                                              limit 1)
                                        end as playtime
                               from ranked_save_states rs
                               group by map_id)
select map_id::text as map_id,
       progress::int8 + 1 as progress,
       playtime::int8 as playtime
from progress_and_playtime;

-- name: InsertMapReport :one
insert into map_reports (map_id, player_id, time, categories, comment)
values ($1, $2, now(), $3, $4)
returning *;

-- name: SearchMaps :many
select sqlc.embed(maps_published),
       count(*) over () as total_count
from maps_published
where listed = true
  and opt_variant = any (@variants::varchar[])
  and (owner = sqlc.narg('owner') or sqlc.narg('owner') is null)
  and (opt_name ~* sqlc.narg('name')::text or coalesce(@name, '') = '')
  and (contest = sqlc.narg('contest') or sqlc.narg('contest') is null)
  and (quality_override = any (@quality::int[]) or coalesce(cardinality(@quality::int[]), 0) = 0)
  and (difficulty = any (@difficulty::int[]) or coalesce(cardinality(@difficulty::int[]), 0) = 0)
order by case when @sort::text = 'random' then random() end,
         case when @sort::text = 'best' and @sort_order::text = 'desc' then -quality_override end,
         case when @sort::text = 'best' and @sort_order::text = 'asc' then quality_override end,
         case when @sort::text = 'best' then -total_likes end,
         case when @sort::text = 'best' then extract(epoch from published_at) * -1 end,
         case
           when @sort::text = 'published' and @sort_order::text = 'desc' then extract(epoch from published_at) * -1 end,
         case when @sort::text = 'published' and @sort_order::text = 'asc' then extract(epoch from published_at) end
offset sqlc.arg('page')::int * sqlc.arg('page_size')::int limit sqlc.arg('page_size')::int;
