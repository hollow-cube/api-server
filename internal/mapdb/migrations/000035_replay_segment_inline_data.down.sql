begin;

-- Fails if any inline segment exists: those bytes only live in this column, so
-- the down migration refuses rather than silently dropping replay data.
alter table replay_segments
  drop constraint replay_segment_storage_exclusive,
  drop column data,
  alter column object_reference set not null;

commit;
