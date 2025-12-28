-- Friends

-- name: GetPlayerFriends :many
select pf.target_id, pd.username, pf.created_at
from player_friends pf
         join player_data pd on pd.id = pf.target_id
where pf.player_id = $1;

-- name: CreatePlayerFriend :exec
insert into player_friends (player_id, target_id)
values ($1, $2);

-- name: DeletePlayerFriend :execrows
delete
from player_friends
where player_id = $1
  and target_id = $2;

-- name: DeletePlayerFriendBidirectional :execrows
delete
from player_friends
where (player_id = $1 and target_id = $2)
   or (player_id = $2 and target_id = $1);

-- name: FriendshipExists :one
select exists (select 1
               from player_friends
               where player_id = $1
                 and target_id = $2);

-- Friend Requests

-- name: GetIncomingFriendRequests :many
select pfr.player_id, pd.username, pfr.created_at
from player_friend_requests pfr
         join player_data pd on pd.id = pfr.player_id
where pfr.target_id = $1;

-- name: GetOutgoingFriendRequests :many
select pfr.target_id, pd.username, pfr.created_at
from player_friend_requests pfr
         join player_data pd on pd.id = pfr.target_id
where pfr.player_id = $1;

-- name: CreateFriendRequest :exec
insert into player_friend_requests (player_id, target_id)
values ($1, $2);

-- name: DeleteFriendRequest :execrows
delete
from player_friend_requests
where player_id = $1
  and target_id = $2;

-- name: DeleteFriendRequestBidirectional :execrows
delete
from player_friend_requests
where (player_id = $1 and target_id = $2)
   or (player_id = $2 and target_id = $1);

-- name: FriendRequestExists :one
select exists (select 1
               from player_friend_requests
               where player_id = $1
                 and target_id = $2);