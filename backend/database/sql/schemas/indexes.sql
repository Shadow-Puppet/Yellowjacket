CREATE INDEX IF NOT EXISTS idx_playlist_tracks_playlist_id
    ON playlist_tracks(playlist_id);

CREATE INDEX IF NOT EXISTS idx_playlist_tracks_audio_file_id
    ON playlist_tracks(audio_file_id);

CREATE INDEX IF NOT EXISTS idx_audio_files_recording_id
    ON audio_files(recording_id);

CREATE INDEX IF NOT EXISTS idx_recordings_artist_credit_id
    ON recordings(artist_credit_id);

CREATE INDEX IF NOT EXISTS idx_release_group_recordings_recording_id
    ON release_group_recordings(recording_id);

CREATE INDEX IF NOT EXISTS idx_release_group_recordings_release_group_id
    ON release_group_recordings(release_group_id);

CREATE INDEX IF NOT EXISTS idx_release_groups_cover_art_id
    ON release_groups(cover_art_id);

CREATE INDEX IF NOT EXISTS idx_release_groups_album_artist_credit_id
    ON release_groups(album_artist_credit_id);

CREATE INDEX IF NOT EXISTS idx_queue_tracks_audio_file_id
    ON queue_tracks(audio_file_id);

CREATE INDEX IF NOT EXISTS idx_artist_credit_artist_artist_id
    ON artist_credit_artist(artist_id);

CREATE INDEX IF NOT EXISTS idx_artist_credit_artist_credit_id
    ON artist_credit_artist(credit_id);
