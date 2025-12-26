CREATE TYPE relationship_status AS ENUM ('pending', 'friend', 'blocked');

CREATE TABLE player_relationship (
    player_id uuid not null references player_data(id),
    target_id uuid not null references player_data(id),
    status relationship_status not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),

    primary key (player_id, target_id),
    check ( player_id <> target_id ) -- ensure no self-relationships
);
