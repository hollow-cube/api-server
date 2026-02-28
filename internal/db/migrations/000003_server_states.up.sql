create table if not exists server_states
(
    id         text        not null primary key,
    role       text        not null,
    start_time timestamptz not null default now(),
    status     int         not null default 0,
    cluster_ip text        not null default ''
);