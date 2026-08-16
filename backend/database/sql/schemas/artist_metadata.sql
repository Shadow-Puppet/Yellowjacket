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

-- No index on mbid alone: PRIMARY KEY (mbid, source) already has it as
-- its leftmost column, so a second one costs a write per row and serves
-- no read.
