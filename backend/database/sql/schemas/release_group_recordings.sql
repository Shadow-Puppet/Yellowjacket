CREATE TABLE IF NOT EXISTS release_group_recordings (
  id   int PRIMARY KEY,
  release_group_id int NOT NULL,
  recording_id int NOT NULL,
  FOREIGN KEY(release_group_id) REFERENCES release_groups(id),
  FOREIGN KEY(recording_id) REFERENCES recordings(id)
);

