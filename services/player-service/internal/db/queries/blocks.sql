-- name: GetBlockedPlayers :many
select pb.target_id, pd.username, pb.created_at, count(*) over () as total
from player_blocks pb
         join player_data pd on pd.id = pb.target_id
where pb.player_id = $1 limit $2 offset $3;

-- name: CreatePlayerBlock :exec
insert into player_blocks (player_id, target_id)
values ($1, $2);

-- name: DeletePlayerBlock :execrows
delete
from player_blocks
where player_id = $1
  and target_id = $2;

-- name: IsBlocked :one
select exists (select 1
               from player_blocks
               where player_id = $1
                 and target_id = $2);