-- Download clients the user has connected: an slskd daemon, a Lidarr
-- instance, yt-dlp on PATH.  One row per configured instance, so two
-- Prowlarr servers or two Soulseek accounts coexist.
--
-- Secrets (API keys, passwords) are NOT stored here.  They live in a
-- 0600 file keyed by this row's id, so this table can be dumped into a
-- bug report without redaction.  `settings` holds only non-sensitive
-- values (host, port, category, output format) as a JSON object.
--
-- `kind` names the adapter implementation and is looked up in the
-- provider registry at startup; a row whose kind no longer exists is
-- reported to the user rather than silently dropped.


CREATE TABLE IF NOT EXISTS download_providers (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    kind       TEXT    NOT NULL,
    name       TEXT    NOT NULL,
    enabled    INTEGER NOT NULL DEFAULT 1,
    -- priority breaks ties between providers that found equally good
    -- candidates.  Higher wins; 50 is the neutral default.
    priority   INTEGER NOT NULL DEFAULT 50,
    settings   TEXT    NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_download_providers_enabled
    ON download_providers(enabled);
