begin;

drop table if exists player_data;
drop table if exists player_backpack;
drop table if exists player_cosmetics;
drop table if exists pending_verification;
drop table if exists linked_accounts;
drop table if exists tx_log;
drop table if exists tebex_state;
drop table if exists tebex_events;
drop table if exists vote_events;
drop table if exists punishments;
drop table if exists pending_tebex_transactions;

commit;