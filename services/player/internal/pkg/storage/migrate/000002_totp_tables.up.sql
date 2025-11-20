create table if not exists player_totp(
    player_id text primary key not null,
    created_at timestamp with time zone default now(),
    active boolean default false,
    key bytea not null,
    recovery_codes text[] not null,

    foreign key (player_id) references player_data(id) on delete cascade
);
