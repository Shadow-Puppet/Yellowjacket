CREATE TABLE IF NOT EXISTS tagging_items (
  group_key               TEXT PRIMARY KEY,
  library_id              INTEGER NOT NULL,
  track_count             INTEGER NOT NULL DEFAULT 0,
  album_name              TEXT NOT NULL DEFAULT '',
  album_artist            TEXT NOT NULL DEFAULT '',
  disc_number             INTEGER NOT NULL DEFAULT 0,
  best_match_release_mbid TEXT,
  score                   REAL,
  last_checked_at         DATETIME,
  status                  TEXT NOT NULL DEFAULT 'pending'
    CHECK(status IN ('pending', 'matched', 'confirmed', 'skipped')),
  -- cleared_at is set when the user explicitly removes the item
  -- from the queue ("clear completed entries").  Cleared rows are
  -- excluded from the queue list but kept in the table so a
  -- subsequent rescan of the same folder doesn't reset the
  -- review state.
  cleared_at              DATETIME,
  created_at              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(library_id) REFERENCES libraries(id)
);

CREATE INDEX IF NOT EXISTS idx_tagging_items_library_status
    ON tagging_items(library_id, status);

CREATE INDEX IF NOT EXISTS idx_tagging_items_status_pending
    ON tagging_items(library_id) WHERE status = 'pending';
