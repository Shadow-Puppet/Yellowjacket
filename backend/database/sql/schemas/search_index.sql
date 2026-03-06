CREATE VIRTUAL TABLE IF NOT EXISTS search_index USING fts5(
    file_path,
    title,
    artist,
    album,
    content='',
    tokenize='unicode61 remove_diacritics 2'
);
