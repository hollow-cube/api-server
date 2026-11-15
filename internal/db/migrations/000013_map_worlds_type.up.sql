alter table map_worlds add column type text;
create unique index map_worlds_edit_map_idx on map_worlds (map_id) where type = 'editing';
