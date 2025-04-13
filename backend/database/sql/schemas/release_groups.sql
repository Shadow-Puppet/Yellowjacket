CREATE TABLE release_groups (
  id   int PRIMARY KEY,
  name text    NOT NULL,
  FOREIGN KEY(cover_art_id) REFERENCES cover_art(id)
);
