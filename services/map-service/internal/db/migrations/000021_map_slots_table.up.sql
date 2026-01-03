begin;

create table if not exists map_slots
(
  player_id  uuid        not null,
  map_id     uuid        not null references maps (id) on delete cascade,
  index      int         not null default -1,
  created_at timestamptz not null default now(),
  primary key (player_id, map_id)
);

create index if not exists idx_map_slots_player_id on map_slots (player_id);
create unique index idx_map_slots_player_index_unique
  on map_slots (player_id, index)
  where index >= 0;

-- migrate existing entries to map_slots
insert into map_slots (player_id, map_id, index, created_at)
select mpd.id,
       m.id as map_id,
       arr.index - 1 as index,
       now()
from map_player_data mpd
  cross join lateral unnest(mpd.maps) with ordinality as arr(map_id_str, index)
  join maps m on m.id = arr.map_id_str::uuid
where mpd.maps is not null
  and arr.map_id_str is not null
  and arr.map_id_str != ''
on conflict (player_id, map_id) do nothing;

commit;