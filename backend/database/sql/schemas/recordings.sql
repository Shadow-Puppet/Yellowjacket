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
  FOREIGN KEY(artist_credit_id) REFERENCES artist_credit(id)
);
