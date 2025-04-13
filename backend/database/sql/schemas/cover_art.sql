CREATE TABLE cover_art (
  id   int PRIMARY KEY,
  is_embedded bool NOT NULL DEFAULT(false),
  file_path text NOT NULL,
  FOREIGN KEY(file_type_id) REFERENCES file_types(id)
);
