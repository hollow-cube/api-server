-- name: InsertChatMessage :exec
insert into chat_messages (timestamp, server_id, channel, sender, target, content, censored_by, censored_detail)
values ($1, $2, $3, $4, $5, $6, $7, $8);
