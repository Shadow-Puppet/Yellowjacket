CREATE TABLE IF NOT EXISTS artist_credit_artist (
  id   integer PRIMARY KEY,
  artist_id int NOT NULL,
  credit_id int NOT NULL,
  FOREIGN KEY(artist_id) REFERENCES artists(id),
  FOREIGN KEY(credit_id) REFERENCES artist_credit(id)
);

CREATE INDEX IF NOT EXISTS idx_artist_credit_artist_artist_id
    ON artist_credit_artist(artist_id);

CREATE INDEX IF NOT EXISTS idx_artist_credit_artist_credit_id
    ON artist_credit_artist(credit_id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_artist_credit_artist_unique
  ON artist_credit_artist(artist_id, credit_id);
