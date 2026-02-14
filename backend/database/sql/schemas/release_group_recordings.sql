CREATE TABLE IF NOT EXISTS release_group_recordings (
  id               INTEGER PRIMARY KEY,
  release_group_id INTEGER NOT NULL,
  recording_id     INTEGER NOT NULL,
  track_number     INTEGER,
  disc_number      INTEGER,
  FOREIGN KEY(release_group_id) REFERENCES release_groups(id),
  FOREIGN KEY(recording_id) REFERENCES recordings(id)
);
