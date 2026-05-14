create table map_files
(
  map_id       uuid        not null references maps (id) on delete cascade,
  path         text        not null,
  content      bytea       not null,
  content_hash bytea       not null, -- raw sha256, 32 bytes
  content_type text        not null,
  size         integer     not null generated always as (octet_length(content)) stored,
  updated_at   timestamptz not null default now(),
  created_at   timestamptz not null default now(),
  primary key (map_id, path),

  -- sanity checks, these should be validated by the api anyway.
  constraint path_valid check (
    path <> ''
      and path not like '/%'
      and path not like '%..%'
      and length(path) <= 512
    ),
  constraint content_hash_is_sha256 check (octet_length(content_hash) = 32)
);