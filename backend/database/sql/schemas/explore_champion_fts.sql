CREATE VIRTUAL TABLE IF NOT EXISTS explore_champion_fts USING fts5(
  title, artist_name, aliases,
  content='explore_index',
  content_rowid='id',
  tokenize='unicode61 remove_diacritics 2'
    );
