
alter table save_states add column if not exists data_version integer not null default 0;
