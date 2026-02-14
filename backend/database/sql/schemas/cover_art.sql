CREATE TABLE IF NOT EXISTS cover_art (
  id          INTEGER PRIMARY KEY,
  is_embedded BOOL NOT NULL DEFAULT(false),
  file_path   TEXT NOT NULL UNIQUE,
  mime_type   TEXT NOT NULL
);
