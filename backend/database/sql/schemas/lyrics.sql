-- Lyrics for a file, and where they came from.
--
-- These used to be a column on `recordings`, in a table classified
-- `Owned` — data a rescan can rebuild from the files.  That was true of
-- lyrics read out of a USLT frame and false of lyrics fetched from
-- LRCLIB, and nothing recorded which was which, so a library with
-- 24,294 of them could not answer how many were free to rebuild and how
-- many were network traffic waiting to happen.  `source` answers it.
--
-- `recording_mbid` is carried alongside the file id so a future
-- re-import can re-adopt fetched lyrics without asking LRCLIB again;
-- the file id is the key because untagged files have no MBID and are
-- exactly the ones whose lyrics had to be fetched.
CREATE TABLE IF NOT EXISTS lyrics (
  audio_file_id  INTEGER PRIMARY KEY,
  text           TEXT NOT NULL,
  source         TEXT NOT NULL DEFAULT 'tag'
    CHECK(source IN ('tag', 'lrclib')),
  recording_mbid TEXT,
  fetched_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(audio_file_id) REFERENCES audio_files(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_lyrics_recording_mbid
    ON lyrics(recording_mbid) WHERE recording_mbid IS NOT NULL;
