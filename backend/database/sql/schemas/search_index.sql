CREATE VIRTUAL TABLE IF NOT EXISTS search_index USING fts5(
  file_path,
  title,
  artist,
  album,
  content='',
  contentless_delete=1,
  tokenize='unicode61 remove_diacritics 2'
    );
