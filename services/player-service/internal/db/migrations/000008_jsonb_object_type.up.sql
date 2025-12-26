begin;

create domain jsonb_object as jsonb
  check (jsonb_typeof(value) = 'object');

alter table player_recaps
  alter column data type jsonb_object;

commit;