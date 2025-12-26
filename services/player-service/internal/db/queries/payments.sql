-- name: GetPendingTransactionByCheckoutId :one
select basket_id, username
from pending_tebex_transactions
where checkout_id = $1;

-- name: CreatePendingTransaction :exec
insert into public.pending_tebex_transactions (player_id, created_at, username, checkout_id)
values ($1, now(), $2, $3);

-- name: ResolvePendingTransaction :exec
update pending_tebex_transactions
set basket_id = $2
where checkout_id = $1;

-- name: CreateTebexState :execrows
insert into tebex_state (tx_id, changes)
values ($1, $2)
on conflict do nothing;

-- name: LogTebexEvent :exec
insert into tebex_events (event_id, timestamp, raw)
values ($1, $2, $3);
