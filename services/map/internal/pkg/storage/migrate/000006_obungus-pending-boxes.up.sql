begin;

create table if not exists obungus_pending_boxes
(
    id             uuid           not null,
    player_id      uuid    default null,
    created_at     timestamptz    not null,

    name           varchar default null,
    -- IS_LONG, IS_STRAIGHT, IS_RIGHT, IS_LONG_RIGHT
    shape          bit varying(8) not null,
    schematic_data bytea          not null,

    primary key (id)
);

create table if not exists obungus_box_ratings
(
    box_id      uuid    not null,
    player_id   uuid    not null,
    is_rejected boolean not null,
    difficulty  int4    not null,
    quality     int4    not null,

    primary key (box_id, player_id),
    constraint fk_box_id foreign key (box_id) references obungus_pending_boxes (id),
    constraint fk_player_id foreign key (player_id) references map_player_data (id)
);

commit;