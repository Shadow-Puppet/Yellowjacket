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
  -- File mtime as a Unix timestamp in seconds, captured at import.
  -- Compared against the on-disk mtime during a scan to detect files
  -- another application retagged in place.  0 means "never recorded"
  -- (rows predating migration 47) and is treated as not-stale so an
  -- upgrade does not re-import the whole library.
  modified_at int NOT NULL DEFAULT 0,
  FOREIGN KEY(file_type_id) REFERENCES file_types(id),
  FOREIGN KEY(recording_id) REFERENCES recordings(id),
  FOREIGN KEY(library_id) REFERENCES libraries(id)
);

CREATE INDEX IF NOT EXISTS idx_audio_files_basename
    ON audio_files(basename);

CREATE INDEX IF NOT EXISTS idx_audio_files_group_key
    ON audio_files(group_key) WHERE group_key != '';

CREATE INDEX IF NOT EXISTS idx_audio_files_library_id
  ON audio_files(library_id);

CREATE INDEX IF NOT EXISTS idx_audio_files_recording_id
    ON audio_files(recording_id);

CREATE INDEX IF NOT EXISTS idx_audio_files_tag_status_untagged
    ON audio_files(library_id) WHERE tag_status = 'untagged';
