create type save_state_type as enum ('editing', 'playing', 'verifying');

alter table save_states
  alter column type type save_state_type
    using type::save_state_type;
