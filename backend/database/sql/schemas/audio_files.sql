CREATE TABLE IF NOT EXISTS audio_files (
  id integer PRIMARY KEY,
  file_path text NOT NULL UNIQUE,
  length_milliseconds int NOT NULL,
  file_type_id int NOT NULL,
  recording_id int NOT NULL,
  FOREIGN KEY(file_type_id) REFERENCES file_types(id),
  FOREIGN KEY(recording_id) REFERENCES recordings(id)
);
