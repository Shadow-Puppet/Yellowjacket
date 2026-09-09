CREATE TABLE IF NOT EXISTS queue (
    id INTEGER PRIMARY KEY CHECK(id = 1),
    current_position INTEGER NOT NULL DEFAULT 0,
    shuffle_mode BOOLEAN NOT NULL DEFAULT false,
    repeat_mode TEXT NOT NULL DEFAULT 'off',
    shuffle_order TEXT,
    -- What the queue was built from ("Playing from: X"): an album,
    -- playlist, smart playlist, genre or artist, identified by the id
    -- that source_type's namespace gives it.
    source_type TEXT NOT NULL DEFAULT '',
    source_id INTEGER NOT NULL DEFAULT 0,
    source_label TEXT NOT NULL DEFAULT ''
);

-- Singleton row: there is exactly one playback queue.
INSERT OR IGNORE INTO queue (id) VALUES (1);
