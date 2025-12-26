begin;

alter table player_recaps
  alter column data type jsonb;

drop domain jsonb_object cascade;

commit;