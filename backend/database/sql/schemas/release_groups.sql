CREATE TABLE IF NOT EXISTS release_groups (
  id   int PRIMARY KEY,
  name text    NOT NULL,
  cover_art_id int NOT NULL,
  FOREIGN KEY(cover_art_id) REFERENCES cover_art(id)
);
