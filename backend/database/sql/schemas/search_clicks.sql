CREATE TABLE IF NOT EXISTS search_clicks (
    query        TEXT NOT NULL,
    entity_mbid  TEXT NOT NULL,
    entity_type  TEXT NOT NULL,
    click_count  INTEGER NOT NULL DEFAULT 1,
    last_clicked DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (query, entity_mbid)
  );

CREATE INDEX IF NOT EXISTS idx_search_clicks_query
  ON search_clicks(query);
