CREATE TABLE IF NOT EXISTS explore_cache (
    url_key     TEXT PRIMARY KEY,
    response    TEXT NOT NULL,
    mbid        TEXT,
    entity_type TEXT,
    expires_at  DATETIME NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_explore_cache_expires ON explore_cache(expires_at);
CREATE INDEX IF NOT EXISTS idx_explore_cache_mbid ON explore_cache(mbid);
