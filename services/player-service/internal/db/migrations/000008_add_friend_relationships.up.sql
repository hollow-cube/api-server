create table player_friends
(
    player_id  uuid        not null references player_data (id) on delete cascade,
    target_id  uuid        not null references player_data (id) on delete cascade,

    created_at timestamptz not null default now(),

    primary key (player_id, target_id),
    check ( player_id <> target_id ) -- ensure no self-relationships
);

create table player_friend_requests
(
    player_id  uuid        not null references player_data (id) on delete cascade,
    target_id  uuid        not null references player_data (id) on delete cascade,

    created_at timestamptz not null default now(),

    primary key (player_id, target_id),
    check ( player_id <> target_id )
);

create table player_blocks
(
    player_id  uuid        not null references player_data (id) on delete cascade,
    target_id  uuid        not null references player_data (id) on delete cascade,

    created_at timestamptz not null default now(),

    primary key (player_id, target_id),
    check ( player_id <> target_id )
);
