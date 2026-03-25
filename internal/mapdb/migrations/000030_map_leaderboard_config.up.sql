-- Default playtime leaderboard is saved as null.
alter table maps add column if not exists leaderboard jsonb default null;
alter table save_states add column if not exists score double precision default null;
-- we filter on format for the global playtime leaderboard
create index idx_maps_leaderboard_format on maps ((leaderboard->>'format'));
