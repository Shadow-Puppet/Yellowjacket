CREATE TABLE IF NOT EXISTS audio_files (
  id integer PRIMARY KEY,
  file_path text NOT NULL UNIQUE,
  length_milliseconds int NOT NULL,
  file_type_id int NOT NULL,
  recording_id int NOT NULL,
  sample_rate int NOT NULL DEFAULT 0,
  bit_depth int NOT NULL DEFAULT 0,
  channels int NOT NULL DEFAULT 0,
  bitrate int NOT NULL DEFAULT 0,
  file_size int NOT NULL DEFAULT 0,
  basename text NOT NULL DEFAULT '',
  library_id int NOT NULL DEFAULT 0,
  play_count int NOT NULL DEFAULT 0,
  last_played datetime,
  tag_status TEXT NOT NULL DEFAULT 'untagged'
    CHECK(tag_status IN (
      'untagged', 'auto_matched', 'user_confirmed', 'user_skipped_permanent'
    )),
  group_key TEXT NOT NULL DEFAULT '',
  FOREIGN KEY(file_type_id) REFERENCES file_types(id),
  FOREIGN KEY(recording_id) REFERENCES recordings(id),
  FOREIGN KEY(library_id) REFERENCES libraries(id)
);

CREATE INDEX IF NOT EXISTS idx_audio_files_recording_id
    ON audio_files(recording_id);

-- idx_audio_files_library_id is created by migration 6 (not here) because
-- on existing databases this schema file is a no-op (CREATE TABLE IF NOT EXISTS)
-- and the library_id column doesn't exist until the migration adds it.
--
-- idx_audio_files_tag_status_untagged + idx_audio_files_group_key are
-- created by migrations 31 and 32 for the same reason — on a pre-31
-- database the partial index predicates (`WHERE tag_status = '...'`
-- and `WHERE group_key != ''`) would reference columns that don't
-- yet exist, since CREATE TABLE IF NOT EXISTS does not add columns
-- to existing tables.  sqlc still sees the columns above, and fresh
-- DBs pick up the indexes inside the migrations.
