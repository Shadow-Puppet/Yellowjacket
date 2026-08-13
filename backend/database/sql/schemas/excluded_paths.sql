-- Paths the user has removed from the library, which the scanner must
-- not import again.
--
-- "Remove from library" deletes the audio_files row and leaves the file
-- on disk.  Without this table the next scan finds the file, sees no
-- row for it, and imports it again — so the exclusion is not an
-- enhancement, it is what makes the operation mean anything.
--
-- A row is keyed by (library_id, file_path) rather than by audio_file
-- id, because the row it names has just been deleted.  ON DELETE
-- CASCADE from libraries means removing a library takes its exclusions
-- with it; a full rescan clears the table outright, which is the only
-- way back for a path removed by mistake until there is a UI for it.

CREATE TABLE IF NOT EXISTS excluded_paths (
    id          INTEGER PRIMARY KEY,
    library_id  INTEGER NOT NULL,
    file_path   TEXT NOT NULL,
    excluded_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(library_id, file_path),
    FOREIGN KEY(library_id) REFERENCES libraries(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_excluded_paths_library
    ON excluded_paths(library_id);
