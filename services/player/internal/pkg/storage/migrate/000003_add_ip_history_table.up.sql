create table if not exists ip_history(
    player_id text not null references player_data(id) on delete cascade,
    address text not null,
    first_seen timestamp not null,
    last_seen timestamp not null,
    seen_count integer not null,

    primary key (player_id, address)
)