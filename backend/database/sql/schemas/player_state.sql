CREATE TABLE IF NOT EXISTS player_state (
    id INTEGER PRIMARY KEY CHECK(id = 1),
    volume INTEGER NOT NULL DEFAULT 50,
    muted BOOLEAN NOT NULL DEFAULT false,
    last_track_path TEXT NOT NULL DEFAULT '',
    last_position_seconds INTEGER NOT NULL DEFAULT 0
);

-- Singleton row: player state is a single mutable record.
INSERT OR IGNORE INTO player_state (id) VALUES (1);
