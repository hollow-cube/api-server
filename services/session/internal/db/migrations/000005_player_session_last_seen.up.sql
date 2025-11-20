alter table player_sessions add column if not exists last_seen timestamp with time zone default now();
