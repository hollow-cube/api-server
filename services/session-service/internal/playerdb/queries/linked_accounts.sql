-- name: LookupPlayerDataBySocialId :one
select player_data.*
from player_data
  inner join linked_accounts linked on linked.player_id = player_data.id
where linked.type = $1
  and linked.social_id = $2;

-- name: LookupSocialIdByPlayerId :one
select social_id
from linked_accounts
where type = $1
  and player_id = $2;

-- name: AddLinkedAccount :exec
insert into linked_accounts (player_id, social_id, type)
values ($2, $3, $1);

-- name: GetPendingVerificationBySecret :one
select type, user_id, user_secret, expiration
from pending_verification
where type = $1
  and user_secret = $2;

-- name: UpsertPendingVerification :exec
insert into pending_verification (type, user_id, user_secret, expiration)
values ($1, $2, $3, $4)
on conflict (type, user_id)
  do update set user_secret = $3,
                expiration  = $4;

-- name: DeletePendingVerificationBySecret :exec
delete
from pending_verification
where type = $1
  and user_secret = $2;
