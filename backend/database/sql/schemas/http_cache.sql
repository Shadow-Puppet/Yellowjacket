-- Short-lived HTTP response cache (search results, MB/LB lookups, etc).
-- For long-lived enrichment data keyed by MBID, see artist_metadata.sql.


CREATE TABLE IF NOT EXISTS http_cache (
    url_key     TEXT PRIMARY KEY,
    response    BLOB NOT NULL,
    expires_at  DATETIME NOT NULL,
    entity_mbid TEXT NOT NULL DEFAULT '',
    entity_type TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_http_cache_expires ON http_cache(expires_at);

CREATE INDEX IF NOT EXISTS idx_http_cache_mbid ON http_cache(entity_mbid);
