-- Friends

-- name: GetPlayerFriends :many
select pf.target_id, pd.username, pd.online, pd.last_online, pf.created_at, count(*) over () as total_count
from player_friends pf
         join player_data pd on pd.id = pf.target_id
where pf.player_id = $1
order by pd.online desc,    -- ensure online friends are listed first
         pf.created_at desc -- just here for consistency in ordering
limit $2 offset $3;

-- name: CreatePlayerFriend :exec
insert into player_friends (player_id, target_id)
values ($1, $2);

-- name: FriendshipExists :one
select exists (select 1
               from player_friends
               where player_id = $1
                 and target_id = $2);

-- name: GetPlayerFriendUsage :one
select (select count(*) from player_friends where player_friends.player_id = $1)::int as friend_count,
       (select count(*)
        from player_friend_requests
        where player_friend_requests.player_id = $1)::int                             as outgoing_friend_request_count;

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

-- Friend Requests

-- name: GetIncomingFriendRequests :many
select pfr.player_id, pd.username, pfr.created_at, count(*) over () as total_count
from player_friend_requests pfr
         join player_data pd on pd.id = pfr.player_id
where pfr.target_id = $1
limit $2 offset $3;

-- name: GetOutgoingFriendRequests :many
select pfr.target_id, pd.username, pfr.created_at, count(*) over () as total_count
from player_friend_requests pfr
         join player_data pd on pd.id = pfr.target_id
where pfr.player_id = $1
limit $2 offset $3;

-- name: CreateFriendRequest :exec
insert into player_friend_requests (player_id, target_id)
values ($1, $2);

-- name: DeleteFriendRequest :one
WITH deleted AS (
    DELETE FROM player_friend_requests
        WHERE player_id = $1
            AND target_id = $2
        RETURNING *)
SELECT d.*, pd.username
FROM deleted d
         JOIN player_data pd ON pd.id = d.player_id;

-- name: DeleteFriendRequestBidirectional :one
WITH deleted AS (
    DELETE FROM player_friend_requests
        WHERE (player_id = $1 AND target_id = $2)
            OR (player_id = $2 AND target_id = $1)
        RETURNING *)
SELECT d.*, pd.username
FROM deleted d
         JOIN player_data pd ON pd.id = d.player_id;

-- name: FriendRequestExists :one
select exists (select 1
               from player_friend_requests
               where player_id = $1
                 and target_id = $2);