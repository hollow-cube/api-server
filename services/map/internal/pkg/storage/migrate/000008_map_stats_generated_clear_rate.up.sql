ALTER TABLE public.map_stats ADD COLUMN IF NOT EXISTS clear_rate float8 GENERATED ALWAYS AS (win_count::float8 / NULLIF(play_count, 0)::float8) STORED;
