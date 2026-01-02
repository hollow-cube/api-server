create table if not exists player_notifications
(
    id         uuid primary key,
    player_id  uuid        not null references player_data (id),
    type       varchar     not null,
    key        varchar     not null,
    data       jsonb                default null,
    created_at timestamptz not null default now(),
    read_at    timestamptz          default null,
    expires_at timestamptz          default null,
    deleted_at timestamptz          default null
)