CREATE TABLE IF NOT EXISTS release_group_recordings (
  id               INTEGER PRIMARY KEY,
  release_group_id INTEGER NOT NULL,
  recording_id     INTEGER NOT NULL,
  track_number     INTEGER,
  disc_number      INTEGER,
  -- The denominator the file's own tag declared: the 12 in "5/12", per
  -- disc.  Read off every file at scan and, until now, discarded — so
  -- "do I have all of this album" had no local answer and the album
  -- page asked MusicBrainz.  NULL means the tag did not say, which is
  -- a third state and not the same as zero.
  total_tracks     INTEGER,
  FOREIGN KEY(release_group_id) REFERENCES release_groups(id),
  FOREIGN KEY(recording_id) REFERENCES recordings(id)
);

CREATE INDEX IF NOT EXISTS idx_release_group_recordings_recording_id
    ON release_group_recordings(recording_id);

CREATE INDEX IF NOT EXISTS idx_release_group_recordings_release_group_id
    ON release_group_recordings(release_group_id);
