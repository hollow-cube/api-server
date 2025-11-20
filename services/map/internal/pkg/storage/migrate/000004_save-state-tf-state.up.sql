begin;

create table if not exists tf_player_session
(
    player_id uuid  not null,
    state     bytea not null,

    primary key (player_id),
    constraint fk_player_id foreign key (player_id) references map_player_data (id) -- does not reference map_player_data because entries in that table are lazy.
);

create table if not exists tf_local_session
(
    player_id uuid  not null,
    world_id  uuid  not null,
    state     bytea not null,

    primary key (player_id, world_id),
    constraint fk_player_id foreign key (player_id) references map_player_data (id) -- does not reference map_player_data because entries in that table are lazy.
);

create table if not exists tf_schematics
(
    player_id  uuid    not null,
    name       varchar not null,
    dimensions bigint  not null,                                                    -- XYZ encoded as a long
    size       bigint  not null,                                                    -- number of bytes in the data
    schem_data bytea   not null,

    primary key (player_id, name),
    constraint fk_player_id foreign key (player_id) references map_player_data (id) -- does not reference map_player_data because entries in that table are lazy.
);

commit;