create table if not exists map_worlds
(
  id         text        not null primary key,
  map_id     uuid        not null references maps (id) on delete cascade,
  server_id  text        not null references server_states (id) on delete cascade,
  created_at timestamptz not null default now()
)