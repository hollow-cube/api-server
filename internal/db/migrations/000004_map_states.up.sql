create table if not exists map_states
(
    id uuid not null primary key,
    map_id text not null,
    server text not null references server_states(id) on update cascade on delete cascade,
    state text not null -- playing, verifying, editing, etc. Not a strict enum but we generally query a particular state.
);