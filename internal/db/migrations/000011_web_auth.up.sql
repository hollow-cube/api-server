create type api_client_kind as enum ('web', 'desktop');

create table api_clients
(
  id           uuid            not null primary key default gen_random_uuid(),
  kind         api_client_kind not null,
  public_key   bytea           not null,        -- DER SPKI of the client's public key
  key_id       text            not null unique, -- RFC 7638 JWK thumbprint (base64url)
  label        text,
  created_at   timestamptz     not null             default now(),
  last_seen_at timestamptz     not null             default now()
);

create table api_sessions
(
  id                  uuid        not null primary key default gen_random_uuid(),
  client_id           uuid        not null references api_clients (id) on delete cascade,
  player_id           uuid        not null references player_data (id) on delete cascade,
  created_at          timestamptz not null             default now(),
  last_active_at      timestamptz not null             default now(),
  idle_expires_at     timestamptz not null,
  absolute_expires_at timestamptz not null,
  revoked_at          timestamptz
);

-- For game -> web/desktop auth handoff
create table api_launch_grants
(
  id                  uuid primary key     default gen_random_uuid(),
  code_hash           bytea       not null unique, -- sha256(launch_code)
  player_id           uuid        not null references player_data (id) on delete cascade,
  map_id              uuid,                        -- set if this is for a specific map (eg while inside a map)
  created_at          timestamptz not null default now(),
  expires_at          timestamptz not null,
  redeemed_at         timestamptz,
  redeemed_session_id uuid references api_sessions (id)
);
