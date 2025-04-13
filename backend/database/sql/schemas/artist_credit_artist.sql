CREATE TABLE artist_credit_artist (
  id   int PRIMARY KEY,
  FOREIGN KEY(artist_id) REFERENCES artists(id),
  FOREIGN KEY(credit_id) REFERENCES artist_credit(id)
);
