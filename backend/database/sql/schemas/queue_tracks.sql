CREATE TABLE IF NOT EXISTS queue_tracks (
    id INTEGER PRIMARY KEY,
    audio_file_id INTEGER NOT NULL,
    position INTEGER NOT NULL,
    FOREIGN KEY(audio_file_id) REFERENCES audio_files(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_queue_tracks_audio_file_id
    ON queue_tracks(audio_file_id);
