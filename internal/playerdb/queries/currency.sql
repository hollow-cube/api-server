-- name: Unsafe_AppendTxLog :exec
insert into tx_log (player_id, timestamp, reason, currency, amount, meta)
values ($1, now(), $2, $3, $4, $5);

-- name: Unsafe_AddCoins :many
update player_data
set coins = coins + @delta
where id = @id
  and coins + @delta >= 0
returning coins;

-- name: Unsafe_AddCubits :many
update player_data
set cubits = cubits + @delta
where id = @id
  and cubits + @delta >= 0
returning cubits;

-- name: GetUnlockedCosmetics :many
select cosmetic_path
from player_cosmetics
where player_id = $1;

-- name: UnlockCosmetic :exec
insert into player_cosmetics (player_id, cosmetic_path, unlocked_at)
values ($1, $2, now());
