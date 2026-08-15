-- Attempt statistics carried across a player's save state lineage on a map. A hard reset creates
-- a fresh save state (and the previous one is often never written), so the latest save state
-- carries the aggregate: how many hard resets preceded it, and the total active playtime across
-- all of them. total_playtime is never zeroed, unlike playtime on only-sprint maps.
alter table save_states
  add column if not exists resets         integer not null default 0,
  add column if not exists total_playtime bigint  not null default 0;
