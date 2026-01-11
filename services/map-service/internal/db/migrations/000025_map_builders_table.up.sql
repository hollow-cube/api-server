begin;

create table if not exists map_builders
(
  map_id     uuid        not null references maps (id) on delete cascade,
  player_id  uuid        not null,
  created_at timestamptz not null default now(),
  is_pending bool                 default true,
  primary key (map_id, player_id)
);

commit;