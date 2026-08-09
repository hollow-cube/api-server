-- name: GetReplay :one
select *
from replays
where id = $1;

-- name: GetReplayForUpdate :one
select *
from replays
where id = $1
for update;

-- name: CreateReplayIfAbsent :one
insert into replays (
  id,
  version,
  recording_revision,
  state,
  representation,
  next_segment_index,
  current_preamble,
  current_preamble_digest
)
values (
  sqlc.arg('id'),
  1,
  1,
  sqlc.arg('state'),
  'segmented',
  sqlc.arg('next_segment_index'),
  sqlc.arg('current_preamble'),
  sqlc.arg('current_preamble_digest')
)
on conflict (id) do nothing
returning *;

-- name: UpdateReplayRecording :one
update replays
set version = version + 1,
    recording_revision = recording_revision + 1,
    state = sqlc.arg('state'),
    next_segment_index = sqlc.arg('next_segment_index'),
    current_preamble = sqlc.arg('current_preamble'),
    current_preamble_digest = sqlc.arg('current_preamble_digest'),
    updated_at = now()
where id = sqlc.arg('id')
returning *;

-- name: PublishReplayCompacted :one
update replays
set version = version + 1,
    representation = 'compacted',
    current_preamble = sqlc.arg('current_preamble'),
    current_preamble_digest = sqlc.arg('current_preamble_digest'),
    compacted_source_revision = recording_revision,
    compacted_object = sqlc.arg('compacted_object'),
    compacted_length = sqlc.arg('compacted_length'),
    compacted_digest = sqlc.arg('compacted_digest'),
    updated_at = now()
where id = sqlc.arg('id')
returning *;

-- name: CreateReplaySegment :exec
insert into replay_segments (
  replay_id,
  segment_index,
  object_reference,
  data,
  length,
  digest,
  commit_revision
)
values (
  sqlc.arg('replay_id'),
  sqlc.arg('segment_index'),
  sqlc.narg('object_reference'),
  sqlc.narg('data'),
  sqlc.arg('length'),
  sqlc.arg('digest'),
  sqlc.arg('commit_revision')
);

-- name: GetReplaySegment :one
select *
from replay_segments
where replay_id = $1
  and segment_index = $2;

-- name: GetReplayIdempotency :one
select *
from replay_idempotency
where replay_id = $1
  and idempotency_key = $2;

-- name: CreateReplayIdempotency :exec
insert into replay_idempotency (
  replay_id,
  idempotency_key,
  request_fingerprint,
  response_status,
  response_etag,
  response_metadata
)
values (
  sqlc.arg('replay_id'),
  sqlc.arg('idempotency_key'),
  sqlc.arg('request_fingerprint'),
  sqlc.arg('response_status'),
  sqlc.arg('response_etag'),
  sqlc.arg('response_metadata')
);
