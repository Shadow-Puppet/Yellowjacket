CREATE TABLE IF NOT EXISTS artist_credit_artist (
  id   int PRIMARY KEY,
  artist_id int NOT NULL,
  credit_id int NOT NULL,
  FOREIGN KEY(artist_id) REFERENCES artists(id),
  FOREIGN KEY(credit_id) REFERENCES artist_credit(id)
);
