-- One row per track *exit*, three ways a listen can end: it reached
-- the end, it was heard enough to count and then skipped past, or it
-- was abandoned for another track before anyone had really listened.
--
-- This is the source of truth for listening behaviour.  The
-- denormalized `play_count` / `last_played` / `skip_count` /
-- `last_skipped` on audio_files are materialized from it, because the
-- hot read path (track-list sort, the shelves, smart playlists) must
-- not join a log that grows by one row per song forever.
--
-- `kind` is the classification, applied at write time:
--
--   complete   the track reached its natural end, or was skipped in
--              its tail window (the last few seconds of a long fade).
--   play       the scrobble threshold was heard — half the track or
--              four minutes, whichever is less — and the user moved on
--              before the end.
--   skip       the user moved to a different track before that.
--
-- `position_seconds` / `duration_seconds` are the raw reading the
-- classification was made from, kept so a future re-tune of the
-- threshold does not need the events re-recorded.  0/0 on a row means
-- "not captured for this event" (e.g. a natural finish recorded before
-- these columns existed), not "a zero-second track".
CREATE TABLE IF NOT EXISTS listening_events (
    id               INTEGER PRIMARY KEY,
    audio_file_id    INTEGER NOT NULL,
    kind             TEXT NOT NULL DEFAULT 'complete'
                     CHECK (kind IN ('complete', 'play', 'skip')),
    position_seconds INTEGER NOT NULL DEFAULT 0,
    duration_seconds INTEGER NOT NULL DEFAULT 0,
    occurred_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY(audio_file_id) REFERENCES audio_files(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_listening_events_audio_file_id
    ON listening_events(audio_file_id);

-- "What did I listen to this month" walks this, rather than the
-- per-track index above.
CREATE INDEX IF NOT EXISTS idx_listening_events_occurred_at
    ON listening_events(occurred_at);
