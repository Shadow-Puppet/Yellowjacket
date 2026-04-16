-- Long-lived artist enrichment data keyed by MBID and source.
-- Sources: audiodb, fanart, wikidata-p18, wikipedia-lead, mb:artist-rels.
-- No TTL — this data changes very rarely and is the backing store for
-- the artist detail page.
CREATE TABLE IF NOT EXISTS artist_metadata (
    mbid       TEXT NOT NULL,
    source     TEXT NOT NULL,
    data       BLOB NOT NULL,
    fetched_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (mbid, source)
);
CREATE INDEX IF NOT EXISTS idx_artist_metadata_mbid ON artist_metadata(mbid);
