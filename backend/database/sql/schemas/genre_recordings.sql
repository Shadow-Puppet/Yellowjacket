CREATE TABLE IF NOT EXISTS recording_genres (
  id           INTEGER PRIMARY KEY,
  recording_id INTEGER NOT NULL,
  genre_id     INTEGER NOT NULL,
  FOREIGN KEY(recording_id) REFERENCES recordings(id),
  FOREIGN KEY(genre_id) REFERENCES genres(id),
  UNIQUE(recording_id, genre_id)
);

CREATE INDEX IF NOT EXISTS idx_recording_genres_recording_id
    ON recording_genres(recording_id);

CREATE INDEX IF NOT EXISTS idx_recording_genres_genre_id
    ON recording_genres(genre_id);
