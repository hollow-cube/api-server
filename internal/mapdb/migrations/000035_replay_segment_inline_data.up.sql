begin;

-- Small segments are stored inline in postgres so that a commit is a single
-- atomic write. Existing rows keep their object_reference and are unaffected;
-- the read path branches on which column is populated, never on the current
-- inline threshold.
alter table replay_segments
  add column data bytea,
  alter column object_reference drop not null,
  add constraint replay_segment_storage_exclusive check (
    (data is null) <> (object_reference is null)
  );

commit;
