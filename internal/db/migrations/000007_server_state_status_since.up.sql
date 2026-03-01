alter table server_states alter column status_v2 set not null;
alter table server_states add column status_since timestamptz not null default now();
