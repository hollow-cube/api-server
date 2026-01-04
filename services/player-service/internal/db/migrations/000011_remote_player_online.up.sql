alter table player_data add column online boolean not null default false;
alter table player_data alter column online drop default;

comment on column player_data.online is 'Updated by observing session status messages from the session-service'