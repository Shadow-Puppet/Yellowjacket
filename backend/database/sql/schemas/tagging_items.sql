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
  -- synthetic marks a group carved out of a "mixed bag" folder by
  -- SplitMixedFolder: its tracks share a folder with unrelated
  -- tracks (a junk-drawer directory) but were clustered together by
  -- matching album/album-artist tags rather than by directory.
  -- Scoring relaxes the missing-track penalty for these groups,
  -- since they're a subset pulled out of a bigger folder, not a
  -- complete rip of their own directory.  parent_group_key is the
  -- original folder group they were split from.
  --
  -- These two columns are declared LAST, after created_at, even
  -- though that reads oddly next to the rest of the table: sql/
  -- migrations/0001 brings a pre-existing tagging_items up to date
  -- with `ALTER TABLE ADD COLUMN`, which SQLite always appends at
  -- the end of the column list.  A fresh install (this file) and an
  -- upgraded database (this file + the migration) must end up with
  -- IDENTICAL column order, because sqlc-generated `SELECT *` scans
  -- (e.g. GetTaggingItem) bind columns positionally — see the
  -- schema/migration column-order test in database_test.go.  Put
  -- new columns wherever reads best when adding a table for the
  -- first time; append-only from the second migration on.
  synthetic               INTEGER NOT NULL DEFAULT 0,
  parent_group_key        TEXT NOT NULL DEFAULT '',
  -- album_artist_conflict latches to 1 the first time two tracks
  -- added to this group carry different non-empty album_artist tags,
  -- and never resets. Without it, UpsertTaggingItemOnTrackAdd's
  -- consensus tracking on album_artist can't tell "no non-empty
  -- value contributed yet" apart from "conflicting values were
  -- observed and it was cleared" — both look like ''  — so a later
  -- track that happens to repeat an earlier, already-disputed value
  -- would wrongly resurrect trust in it. See IsMixedBag
  -- (backend/autotag/mixedbag.go), which trusts a non-empty
  -- album_artist unconditionally.
  album_artist_conflict   INTEGER NOT NULL DEFAULT 0,
  FOREIGN KEY(library_id) REFERENCES libraries(id)
);

CREATE INDEX IF NOT EXISTS idx_tagging_items_library_status
    ON tagging_items(library_id, status);

CREATE INDEX IF NOT EXISTS idx_tagging_items_status_pending
    ON tagging_items(library_id) WHERE status = 'pending';

-- idx_tagging_items_parent_group_key is NOT declared here on
-- purpose: this file runs unconditionally, before migrations, even
-- against a database that hasn't run 0001 yet — an index predicate
-- referencing parent_group_key would fail on that table.  It lives
-- solely in sql/migrations/0001_tagging_items_synthetic.sql, which
-- runs after the column exists either way (see database.go).
