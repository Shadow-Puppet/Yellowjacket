CREATE TABLE IF NOT EXISTS release_to_rg (
  release_mbid TEXT PRIMARY KEY,
  rg_mbid      TEXT NOT NULL
    ) WITHOUT ROWID;
