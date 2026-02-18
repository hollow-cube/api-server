alter table tf_player_session drop constraint fk_player_id;
alter table tf_local_session drop constraint fk_player_id;
alter table tf_schematics drop constraint fk_player_id;
