CREATE TABLE release_group_recordings (
  id   int PRIMARY KEY,
  FOREIGN KEY(release_group_id) REFERENCES release_groups(id),
  FOREIGN KEY(recording_id) REFERENCES recordings(id)
);

