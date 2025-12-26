-- name: GetRelationships :many
select pr.target_id as player_id, pd.username, pr.created_at
from player_relationship pr
         join player_data pd on pd.id = pr.target_id
where pr.player_id = $1
  and pr.status = $2;

-- name: GetIncomingFriendRequests :many
select pr.player_id, pd.username, pr.created_at
from player_relationship pr
         join player_data pd on pd.id = pr.player_id
where pr.target_id = $1
  and pr.status = 'pending';

-- name: GetRelationship :many
select *
from player_relationship
where (player_id = $1 and target_id = $2)
   or (@bidirectional::bool and player_id = $2 and target_id = $1);

-- name: CreateRelationship :one
insert into player_relationship (player_id, target_id, status)
values ($1, $2, $3)
returning *;

-- name: UpdateRelationshipStatus :one
update player_relationship
set status     = $3,
    updated_at = now()
where player_id = $1
  and target_id = $2
returning *;

-- name: DeleteRelationship :execrows
delete
from player_relationship
where (player_id = $1 and target_id = $2)
   or (@bidirectional::bool and player_id = $2 and target_id = $1);