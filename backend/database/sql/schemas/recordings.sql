CREATE TABLE IF NOT EXISTS recordings (
  id               INTEGER PRIMARY KEY,
  name             TEXT NOT NULL,
  artist_credit_id INTEGER NOT NULL,
  track_number     INTEGER,
  disc_number      INTEGER,
  year             INTEGER,
  genre            TEXT,
  composer         TEXT,
  lyrics           TEXT,
  comment          TEXT,
  mbid             TEXT,
  FOREIGN KEY(artist_credit_id) REFERENCES artist_credit(id)
);

CREATE INDEX IF NOT EXISTS idx_recordings_artist_credit_id
    ON recordings(artist_credit_id);

CREATE INDEX IF NOT EXISTS idx_recordings_mbid ON recordings(mbid) WHERE mbid IS NOT NULL;
