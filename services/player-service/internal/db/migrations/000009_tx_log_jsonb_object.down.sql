begin;

alter table tx_log
  alter column meta type jsonb;

commit;
