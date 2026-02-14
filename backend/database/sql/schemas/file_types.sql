CREATE TABLE IF NOT EXISTS file_types (
  id   integer PRIMARY KEY,
  extension text    NOT NULL UNIQUE
);
