CREATE TABLE IF NOT EXISTS play_history (
    id INTEGER PRIMARY KEY,
    audio_file_id INTEGER NOT NULL,
    played_at DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY(audio_file_id) REFERENCES audio_files(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_play_history_audio_file_id
    ON play_history(audio_file_id);
