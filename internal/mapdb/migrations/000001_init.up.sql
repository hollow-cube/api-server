begin;

create table if not exists maps
(
    id               uuid primary key,
    owner            uuid        not null,
    m_type           varchar     not null,
    created_at       timestamptz not null,
    updated_at       timestamptz not null,
    verification     int8                 default 0,
    authz_key        varchar              default null,
    file_id          varchar     not null,
    legacy_map_id    varchar              default null,

    published_id     bigint               default null,
    published_at     timestamptz          default null,

    quality_override int8                 default 0,

    opt_name         varchar              default null,
    opt_icon         varchar              default null,
    size             int8        not null default 0,
    opt_variant      varchar     not null,
    opt_subvariant   varchar              default null,
    opt_spawn_point  varchar     not null,

    opt_only_sprint  bool                 default false,
    opt_no_sprint    bool                 default false,
    opt_no_jump      bool                 default false,
    opt_no_sneak     bool                 default false,
    opt_boat         bool                 default false,
    opt_extra        bytea                default null,

    opt_tags         varchar[]            default null,

    ext              bytea       not null default '{}', -- holds the extended map data

    -- the following are only set if the map is soft deleted
    deleted_at       timestamptz          default null,
    deleted_by       uuid                 default null,
    deleted_reason   varchar              default null
);

create table if not exists save_states
(
    id        uuid        not null,
    map_id    uuid        not null,
    player_id uuid        not null,
    type      varchar     not null,
    created   timestamptz not null,
    updated   timestamptz not null,
    deleted   timestamptz default null,
    completed boolean     not null,
    playtime  bigint      not null,

    state_v2  bytea       not null, -- Holds either editing or playing state, depending on the type, as json right now

    primary key (id, map_id, player_id),
    constraint fk_map_id foreign key (map_id) references maps (id)
);

create table if not exists map_player_data
(
    id              uuid           not null,
    unlocked_slots  int            not null,
    maps            varchar(36)[5] not null,
    last_played_map varchar(36) default null,
    last_edited_map varchar(36) default null,

    primary key (id)
);

create table if not exists map_ratings
(
    map_id    uuid not null references maps (id),
    player_id uuid not null, -- does not reference map_player_data because entries in that table are lazy.
    rating    int  not null, -- 0 = dislike, like = 1
    comment   varchar default null,

    primary key (map_id, player_id)
);

create table if not exists map_stats
(
    map_id     uuid   not null primary key references maps (id),
    play_count bigint not null,
    win_count  bigint not null
);

create table if not exists map_orgs
(
    id          uuid not null,
    webhook_url varchar default null,

    primary key (id)
);

create table if not exists map_reports
(
    id         serial      not null primary key,
    map_id     uuid        not null references maps (id),
    player_id  uuid        not null, -- does not reference map_player_data because entries in that table are lazy.
    time       timestamptz not null,
    categories int[]       not null,
    comment    varchar default null
);

commit;