
alter table tf_schematics add column if not exists filetype varchar(255) not null default 'unknown';
