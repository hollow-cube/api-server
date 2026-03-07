CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX idx_player_data_username_trgm
  ON player_data USING gin (lower(username) gin_trgm_ops);
