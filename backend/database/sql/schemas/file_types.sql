CREATE TABLE IF NOT EXISTS file_types (
  id   integer PRIMARY KEY,
  extension text    NOT NULL UNIQUE
);

INSERT OR IGNORE INTO file_types (id, extension) VALUES (0, '.mp3');
INSERT OR IGNORE INTO file_types (id, extension) VALUES (1, '.flac');
INSERT OR IGNORE INTO file_types (id, extension) VALUES (2, '.ogg');
INSERT OR IGNORE INTO file_types (id, extension) VALUES (3, '.wav');
