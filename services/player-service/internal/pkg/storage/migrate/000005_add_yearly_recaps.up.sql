create table if not exists player_recaps(
    id        text  not null,
    player_id text  not null,
    year      int   not null,
    data      jsonb not null,

    primary key (id),
    foreign key (player_id) references player_data (id) on delete cascade
);
