-- Clears all entries where the username was put in as the player ID by mistake
delete from pending_tebex_transactions where char_length(player_id) != 36;