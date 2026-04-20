CREATE INDEX idx_maps_published_std_leaderboard ON maps (id)
    WHERE published_at IS NOT NULL
        AND deleted_at IS NULL
        AND COALESCE(leaderboard ->> 'format', 'time') = 'time'
        AND COALESCE(leaderboard ->> 'asc', 'true') = 'true';

CREATE INDEX idx_save_states_completed_runs
    ON save_states (map_id)
    WHERE deleted IS NULL
        AND completed = true
        AND playtime != 0
        AND (type = 'playing' OR type = 'verifying');