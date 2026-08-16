-- Genres per file.  This is `recording_genres` with the recording taken
-- out of the middle: it is the one many-to-many in the local library
-- that is actually many-to-many (a real library runs about four genre
-- rows per file), which is why it stays a join table when the others
-- did not.
CREATE TABLE IF NOT EXISTS file_genres (
  audio_file_id INTEGER NOT NULL,
  genre_id      INTEGER NOT NULL,
  PRIMARY KEY (audio_file_id, genre_id),
  FOREIGN KEY(audio_file_id) REFERENCES audio_files(id) ON DELETE CASCADE,
  FOREIGN KEY(genre_id)      REFERENCES genres(id)
) WITHOUT ROWID;

-- The reverse direction ("which files are in this genre").  The
-- forward direction is served by the primary key, so — unlike the
-- table this replaces — there is no third index restating it.
CREATE INDEX IF NOT EXISTS idx_file_genres_genre_id
    ON file_genres(genre_id);
