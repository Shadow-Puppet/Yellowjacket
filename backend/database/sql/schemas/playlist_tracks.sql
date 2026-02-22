CREATE TABLE IF NOT EXISTS playlist_tracks (
    id INTEGER PRIMARY KEY,
    playlist_id INTEGER NOT NULL,
    audio_file_id INTEGER NOT NULL,
    position INTEGER NOT NULL,
    FOREIGN KEY(playlist_id) REFERENCES playlists(id) ON DELETE CASCADE,
    FOREIGN KEY(audio_file_id) REFERENCES audio_files(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_playlist_tracks_playlist_id
    ON playlist_tracks(playlist_id);

CREATE INDEX IF NOT EXISTS idx_playlist_tracks_audio_file_id
    ON playlist_tracks(audio_file_id);
