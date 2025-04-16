CREATE TABLE IF NOT EXISTS file_types (
  id   INTEGER PRIMARY KEY,
  extension text    NOT NULL UNIQUE
);
