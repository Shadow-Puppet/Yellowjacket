CREATE TABLE recordings (
  id   int PRIMARY KEY,
  name text    NOT NULL,
  FOREIGN KEY(artist_credit_id) REFERENCES artist_credit(id)
);

