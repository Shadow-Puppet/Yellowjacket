CREATE TABLE audio_files (
  id uuid PRIMARY KEY,
  file_path text NOT NULL,
  file_type text  NOT NULL,
  length_seconds int NOT NULL
);
