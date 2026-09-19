-- 0 until the tracker has pinged the server. The proxy tells ViaVersion what each backend speaks
-- from this, so a rollout can leave the fleet on two versions.
alter table server_states add column protocol_version int not null default 0;
