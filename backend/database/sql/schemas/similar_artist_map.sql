CREATE TABLE IF NOT EXISTS similar_artist_map (
  source_artist_mbid  TEXT NOT NULL,
  similar_artist_mbid TEXT NOT NULL,
  similar_artist_name TEXT NOT NULL,
  score               INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (source_artist_mbid, similar_artist_mbid)
    );

CREATE INDEX IF NOT EXISTS idx_similar_artist_map_source
    ON similar_artist_map(source_artist_mbid);
