CREATE TABLE IF NOT EXISTS queue (
    id INTEGER PRIMARY KEY CHECK(id = 1),
    source_playlist_id INTEGER,
    current_position INTEGER NOT NULL DEFAULT 0,
    shuffle_mode BOOLEAN NOT NULL DEFAULT false,
    repeat_mode TEXT NOT NULL DEFAULT 'off',
    shuffle_order TEXT,
    FOREIGN KEY(source_playlist_id) REFERENCES playlists(id) ON DELETE SET NULL
);

INSERT OR IGNORE INTO queue (id) VALUES (1);
