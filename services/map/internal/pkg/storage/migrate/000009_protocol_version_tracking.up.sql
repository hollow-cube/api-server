-- This was added in 1.21.5 aka 770, but we may back-support 1.21.4 (it seems pretty free to do so)
-- so we default to 1.21.4 aka 769.

alter table maps add column if not exists protocol_version int default 769;
alter table save_states add column if not exists protocol_version int default 769;
