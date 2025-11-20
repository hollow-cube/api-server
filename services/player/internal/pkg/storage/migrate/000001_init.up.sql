create table if not exists player_data
(
    id           varchar(36)   not null,
    username     varchar       not null,
    first_join   timestamptz   not null,
    last_online  timestamptz   not null,
    playtime     bigint        not null default 0,
    experience   bigint        not null default 0,
    ip_history   varchar(15)[] not null,
    beta_enabled boolean                default false,
    settings     jsonb         not null default '{}',

    -- currency --
    coins        bigint        not null default 0,
    cubits       bigint        not null default 0,
    -- end currency --

    primary key (id)
);
create table if not exists player_backpack
(
    player_id varchar(36) not null references player_data (id),
    -- Schema created from model fields
    primary key (player_id)
);
create table if not exists player_cosmetics
(
    player_id     varchar(36) not null references player_data (id),
    cosmetic_path varchar     not null, -- In the form type/id
    unlocked_at   timestamptz not null,
    primary key (player_id, cosmetic_path)
);

-- Verification
create table if not exists pending_verification
(
    type        varchar     not null, -- "discord"
    user_id     varchar     not null,
    user_secret varchar     not null,
    expiration  timestamptz not null,

    primary key (type, user_id)
);
create table if not exists linked_accounts
(
    player_id varchar(36) not null,
    social_id varchar     not null,
    type      varchar     not null,

    primary key (type, social_id, player_id)
);

-- Currency Logging
-- tx_log is all cubit changes for any action (buying cosmetics, etc)
create table if not exists tx_log
(
    id        serial primary key,
    player_id varchar(36) not null,
    timestamp timestamptz not null,
    reason    varchar     not null, -- "tebex_oneoff", "etc"
    currency  varchar     not null default 'cubits',
    amount    bigint      not null,
    meta      jsonb,

    foreign key (player_id) references player_data (id)
);
-- tebex_state is a record of every applied tebex transaction applied
create table if not exists tebex_state
(
    tx_id    varchar not null primary key, -- tebex transaction id
    changes  jsonb   not null,             -- array of applied changes
    reverted boolean not null default false
);
-- tebex_log contains an entry for every single tebex event we have handled
create table if not exists tebex_events
(
    id        serial primary key,
    event_id  varchar     not null, -- tebex event uuid
    timestamp timestamptz not null,
    raw       bytea       not null
);
create table if not exists vote_events
(
    vote_id   varchar     not null primary key,
    player_id varchar     not null,
    timestamp timestamptz not null,
    source    varchar(50) not null,
    meta      varchar
);

create table if not exists punishments
(
    id             serial primary key,
    player_id      varchar(36) not null,
    executor_id    varchar(36) not null,
    type           varchar(4)  not null, -- "ban", "kick", "mute"
    created_at     timestamptz not null,
    ladder_id      varchar     default null,
    comment        varchar     not null,
    expires_at     timestamptz default null,
    revoked_by     varchar(36) default null,
    revoked_at     timestamptz default null,
    revoked_reason varchar     default null
);
create table if not exists pending_tebex_transactions
(
    player_id   varchar(36) not null,
    created_at  timestamptz not null,
    username    varchar     not null,
    checkout_id varchar     not null,
    basket_id   varchar default null,
    primary key (player_id, checkout_id)
);