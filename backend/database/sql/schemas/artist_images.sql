CREATE TABLE IF NOT EXISTS artist_images (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  artist_mbid TEXT NOT NULL,
  source      TEXT NOT NULL,
  source_url  TEXT NOT NULL,
  file_path   TEXT NOT NULL,
  is_primary  INTEGER NOT NULL DEFAULT 0,
  sort_order  INTEGER NOT NULL DEFAULT 0,
  width       INTEGER,
  height      INTEGER,
  file_size   INTEGER,
  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
    );

-- No index on artist_mbid alone: the UNIQUE index below has it as its
-- leftmost column.

CREATE UNIQUE INDEX IF NOT EXISTS idx_artist_images_source
    ON artist_images(artist_mbid, source, source_url);
