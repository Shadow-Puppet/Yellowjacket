CREATE TABLE IF NOT EXISTS similar_artist_map (
  source_artist_mbid  TEXT NOT NULL,
  similar_artist_mbid TEXT NOT NULL,
  similar_artist_name TEXT NOT NULL,
  score               INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (source_artist_mbid, similar_artist_mbid)
    );

-- No index on source_artist_mbid alone: the PRIMARY KEY has it as its
-- leftmost column.
