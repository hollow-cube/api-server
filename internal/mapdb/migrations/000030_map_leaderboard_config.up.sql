-- Default playtime leaderboard is saved as null.
alter table maps add column if not exists leaderboard jsonb default null;
alter table save_states add column if not exists score double precision default null;
