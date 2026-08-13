CREATE TABLE IF NOT EXISTS queue (
    id INTEGER PRIMARY KEY CHECK(id = 1),
    source_playlist_id INTEGER,
    current_position INTEGER NOT NULL DEFAULT 0,
    shuffle_mode BOOLEAN NOT NULL DEFAULT false,
    repeat_mode TEXT NOT NULL DEFAULT 'off',
    shuffle_order TEXT,
    -- source_playlist_id above is unused dead weight (nothing has ever
    -- written it a nonzero value); source_type/source_id/source_label
    -- below are its generalized replacement, covering albums, playlists,
    -- smart playlists, genres and artists rather than playlists alone.
    source_type TEXT NOT NULL DEFAULT '',
    source_id INTEGER NOT NULL DEFAULT 0,
    source_label TEXT NOT NULL DEFAULT '',
    FOREIGN KEY(source_playlist_id) REFERENCES playlists(id) ON DELETE SET NULL
);

-- Singleton row: there is exactly one playback queue.
INSERT OR IGNORE INTO queue (id) VALUES (1);
