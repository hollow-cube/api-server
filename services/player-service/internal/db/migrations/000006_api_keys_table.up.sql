begin;

-- Drop foreign key constraints
alter table player_cosmetics drop constraint player_cosmetics_player_id_fkey;
alter table player_backpack drop constraint player_backpack_player_id_fkey;
alter table player_totp drop constraint player_totp_player_id_fkey;
alter table tx_log drop constraint tx_log_player_id_fkey;
alter table ip_history drop constraint ip_history_player_id_fkey;
alter table player_recaps drop constraint player_recaps_player_id_fkey;

-- Alter all columns
alter table player_data
  alter column id type uuid using id::uuid;

alter table player_cosmetics
  alter column player_id type uuid using player_id::uuid;

alter table player_backpack
  alter column player_id type uuid using player_id::uuid;

alter table player_totp
  alter column player_id type uuid using player_id::uuid;

alter table tx_log
  alter column player_id type uuid using player_id::uuid;

alter table ip_history
  alter column player_id type uuid using player_id::uuid;

alter table player_recaps
  alter column player_id type uuid using player_id::uuid;

-- Recreate foreign key constraints
alter table player_cosmetics
  add constraint player_cosmetics_player_id_fkey
    foreign key (player_id) references player_data(id);

alter table player_backpack
  add constraint player_backpack_player_id_fkey
    foreign key (player_id) references player_data(id);

alter table player_totp
  add constraint player_totp_player_id_fkey
    foreign key (player_id) references player_data(id);

alter table tx_log
  add constraint tx_log_player_id_fkey
    foreign key (player_id) references player_data(id);

alter table ip_history
  add constraint ip_history_player_id_fkey
    foreign key (player_id) references player_data(id);

alter table player_recaps
  add constraint player_recaps_player_id_fkey
    foreign key (player_id) references player_data(id);

create table if not exists api_keys
(
  id uuid primary key default gen_random_uuid(),
  key_hash text unique not null,
  player_id uuid not null references player_data(id),
  created_at timestamptz default now(),
  deleted_at timestamptz default null
);

commit;
