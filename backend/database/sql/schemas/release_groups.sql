CREATE TABLE IF NOT EXISTS release_groups (
  id                     INTEGER PRIMARY KEY,
  name                   TEXT NOT NULL,
  cover_art_id           INTEGER,
  album_artist_credit_id INTEGER,
  year                   INTEGER,
  total_tracks           INTEGER,
  total_discs            INTEGER,
  mbid                   TEXT,
  FOREIGN KEY(cover_art_id) REFERENCES cover_art(id),
  FOREIGN KEY(album_artist_credit_id) REFERENCES artist_credit(id),
  UNIQUE(name, album_artist_credit_id)
);

CREATE INDEX IF NOT EXISTS idx_release_groups_cover_art_id
    ON release_groups(cover_art_id);

CREATE INDEX IF NOT EXISTS idx_release_groups_album_artist_credit_id
    ON release_groups(album_artist_credit_id);
