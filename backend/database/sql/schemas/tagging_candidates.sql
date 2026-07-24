-- tagging_candidates durably stores the scored candidate list for a
-- tagging group so it survives process restarts.  Without it, only the
-- top score (a single REAL on tagging_items) is persisted; the full
-- candidate list — releases, tracks, alignments — is recomputed every
-- session, re-hitting MusicBrainz whenever the short-TTL http_cache has
-- expired.  The blob is written once when a group is first scored and
-- read back on every subsequent open.
--
-- candidates holds the JSON-encoded []autotag.Candidate.  ON DELETE
-- CASCADE ties the blob's lifetime to its tagging_items row: when a
-- group's tracks change, the scan path deletes the old group_key row
-- (and SQLite, with foreign_keys = ON, drops the stale blob with it).
CREATE TABLE IF NOT EXISTS tagging_candidates (
  group_key   TEXT PRIMARY KEY,
  candidates  TEXT NOT NULL,
  computed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(group_key) REFERENCES tagging_items(group_key) ON DELETE CASCADE
);
