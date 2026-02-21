alter table player_data
  add column hypercube_start timestamptz default null;
alter table player_data
  add column hypercube_end timestamptz default null;

alter table player_data
  add constraint hypercube_start_end_invariant
    check (
      (hypercube_start is null and hypercube_end is null)
        or
      (hypercube_start is not null and hypercube_end is not null)
      );

create type role_type as enum (
  'default', 'hypercube', 'media',
  'ct_1', 'mod_1', 'dev_1',
  'ct_2', 'mod_2', 'dev_2',
  'ct_3', 'mod_3', 'dev_3'
  );

alter table player_data
  add column role role_type not null default 'default';
