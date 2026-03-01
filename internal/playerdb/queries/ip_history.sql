-- name: AddPlayerIP :exec
insert into public.ip_history (player_id, address, first_seen, last_seen, seen_count)
values ($1, $2, now(), now(), 1)
on conflict (player_id, address) do update set last_seen  = excluded.last_seen,
                                               seen_count = ip_history.seen_count + 1;

-- name: GetPlayerIPHistory :many
select address
from public.ip_history
where player_id = $1
order by last_seen desc;

-- name: GetPlayersByIPs :many
select player_data.id, player_data.username
from ip_history
         inner join player_data on ip_history.player_id = player_data.id
where address = ANY ($1::text[])
group by player_data.id;