CREATE TABLE IF NOT EXISTS queue_tracks (
    id INTEGER PRIMARY KEY,
    audio_file_id INTEGER NOT NULL,
    position INTEGER NOT NULL,
    FOREIGN KEY(audio_file_id) REFERENCES audio_files(id) ON DELETE CASCADE
);
