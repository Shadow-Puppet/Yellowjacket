-- One row per audio file, and the file's tags live on it.
--
-- This table used to be a stub — path, format, a foreign key — with
-- every tag-derived field one join away in `recordings`, which was in
-- turn linked to an album through `release_group_recordings` and to an
-- artist through `artist_credit` + `artist_credit_artist`.  That is
-- MusicBrainz's data model, and it is the right model for MusicBrainz:
-- a recording really can appear on many releases and a credit really
-- can list many artists.
--
-- It was the wrong model here, and the library said so.  Measured on a
-- real 25,966-file library: **no** recording had more than one file,
-- **no** recording belonged to more than one release group, and 3 of
-- 2,823 credits listed more than one artist.  Every many-to-many the
-- schema modelled was 1:1 in the data, and the cost of modelling it
-- anyway was a six-way join in every read, a `MIN(release_group_id)`
-- subquery in eleven queries to collapse a fan-out that never happened,
-- a first-credited-artist subquery in nine more to collapse the other
-- one, and — the reason this changed — a whole class of bugs where a
-- `recordings` row **outlived the file that created it**.  Retagging a
-- file created a new recording and abandoned the old one, so the same
-- library carried 812 recordings, 216 release groups and 260 artists
-- with no file behind them, and everything that asked "do I own this"
-- by looking for a metadata row got 129 confident yeses for tracks
-- that could not be played.
--
-- With the tags on the file, ownership is not a rule anyone can forget:
-- the row *is* the file.
CREATE TABLE IF NOT EXISTS audio_files (
  id                 INTEGER PRIMARY KEY,
  file_path          TEXT NOT NULL UNIQUE,
  library_id         INTEGER NOT NULL DEFAULT 0,
  file_type_id       INTEGER NOT NULL,

  -- Audio properties, read from the file itself.
  length_milliseconds INTEGER NOT NULL,
  sample_rate        INTEGER NOT NULL DEFAULT 0,
  bit_depth          INTEGER NOT NULL DEFAULT 0,
  channels           INTEGER NOT NULL DEFAULT 0,
  bitrate            INTEGER NOT NULL DEFAULT 0,
  file_size          INTEGER NOT NULL DEFAULT 0,

  -- Tags.  `artist_credit` is the credit as tagged ("A feat. B") and is
  -- for display; `artist_id` is the primary artist it resolves to, and
  -- is what grouping, browsing and the artist page use.  Keeping both
  -- is what makes the credit table unnecessary: the string is the only
  -- thing that was ever read off it.
  title              TEXT NOT NULL DEFAULT '',
  artist_credit      TEXT NOT NULL DEFAULT '',
  artist_id          INTEGER,
  album_id           INTEGER,
  track_number       INTEGER,
  disc_number        INTEGER,
  -- The denominator the tag declared: the 12 in "5/12", per disc.  It
  -- is what lets "do I have all of this album" be answered from disk
  -- instead of from MusicBrainz.  NULL means the tag did not say, which
  -- is a third state and not the same as zero.
  total_tracks       INTEGER,
  year               INTEGER,
  composer           TEXT NOT NULL DEFAULT '',
  comment            TEXT NOT NULL DEFAULT '',
  recording_mbid     TEXT,

  -- Library bookkeeping.
  basename           TEXT NOT NULL DEFAULT '',
  group_key          TEXT NOT NULL DEFAULT '',
  -- File mtime as a Unix timestamp in seconds, captured at import and
  -- compared against the on-disk mtime during a scan to detect files
  -- another application retagged in place.
  modified_at        INTEGER NOT NULL DEFAULT 0,
  play_count         INTEGER NOT NULL DEFAULT 0,
  last_played        DATETIME,
  tag_status         TEXT NOT NULL DEFAULT 'untagged'
    CHECK(tag_status IN (
      'untagged', 'auto_matched', 'user_confirmed', 'user_skipped_permanent'
    )),

  FOREIGN KEY(file_type_id) REFERENCES file_types(id),
  FOREIGN KEY(library_id)   REFERENCES libraries(id),
  FOREIGN KEY(artist_id)    REFERENCES artists(id),
  FOREIGN KEY(album_id)     REFERENCES albums(id)
);

CREATE INDEX IF NOT EXISTS idx_audio_files_album_id
    ON audio_files(album_id);

CREATE INDEX IF NOT EXISTS idx_audio_files_artist_id
    ON audio_files(artist_id);

CREATE INDEX IF NOT EXISTS idx_audio_files_basename
    ON audio_files(basename);

CREATE INDEX IF NOT EXISTS idx_audio_files_group_key
    ON audio_files(group_key) WHERE group_key != '';

CREATE INDEX IF NOT EXISTS idx_audio_files_library_id
    ON audio_files(library_id);

-- The ownership question, asked by MBID: "is there a *file* with this
-- recording MBID".  Nothing may answer it from a metadata table again.
CREATE INDEX IF NOT EXISTS idx_audio_files_recording_mbid
    ON audio_files(recording_mbid) WHERE recording_mbid IS NOT NULL;

-- Answers "does this tagging group still contain untagged files" in one
-- seek per group.  The autotag queue asks it once per row.
CREATE INDEX IF NOT EXISTS idx_audio_files_untagged_group_key
    ON audio_files(group_key) WHERE tag_status = 'untagged';

CREATE INDEX IF NOT EXISTS idx_audio_files_tag_status_untagged
    ON audio_files(library_id) WHERE tag_status = 'untagged';
