begin;

alter table tx_log
  alter column meta type jsonb_object;

commit;