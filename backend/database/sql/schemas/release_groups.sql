CREATE TABLE IF NOT EXISTS release_groups (
  id                     INTEGER PRIMARY KEY,
  name                   TEXT NOT NULL,
  cover_art_id           INTEGER,
  album_artist_credit_id INTEGER,
  -- year is the *technical release year* of the album as it lives
  -- in the user's library — typically the file's ID3 year tag,
  -- which for remasters/reissues is the reissue year.
  year                   INTEGER,
  -- original_year is the album's *first-release-date* year sourced
  -- from MusicBrainz' release-group.first-release-date.  For a 2010
  -- remaster of a 1973 album, year=2010 and original_year=1973.
  -- Populated by autotag apply; NULL until the user accepts a
  -- candidate (or for libraries that have never been autotagged).
  -- Reads should COALESCE(original_year, year) to get the
  -- preferred user-facing year.
  original_year          INTEGER,
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
