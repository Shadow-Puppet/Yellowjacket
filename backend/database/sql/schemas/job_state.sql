-- Durable state for background jobs. Currently holds one row per job
-- that the user paused, so a paused library scan or search index build
-- comes back paused after a restart instead of silently resuming (or
-- silently never running again).
--
-- Rows are written when a durable job enters the paused state and
-- deleted on resume, cancel, or completion — this is not a job history
-- table, and it stays at zero rows in the common case.
CREATE TABLE IF NOT EXISTS job_state (
    id         TEXT PRIMARY KEY,
    kind       TEXT NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    subtitle   TEXT NOT NULL DEFAULT '',
    paused_at  TEXT NOT NULL
);
