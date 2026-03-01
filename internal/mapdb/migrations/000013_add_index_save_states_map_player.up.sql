-- This query primarily speeds up save state lookups but also helps with many other queries (e.g. top time leaderboards).
CREATE INDEX CONCURRENTLY idx_save_states_map_player
    ON public.save_states (map_id, player_id)
    WHERE deleted IS NULL; -- ignore deleted to save space
