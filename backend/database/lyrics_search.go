package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// toNullString treats an empty string as NULL.
func toNullString(v string) sql.NullString {
	if v == "" {
		return sql.NullString{}
	}

	return sql.NullString{String: v, Valid: true}
}

// LyricsHit is a single result from a lyric-fragment search: the
// matched file plus enough metadata to render and play it.
type LyricsHit struct {
	AudioFileID        int64
	FilePath           string
	LengthMilliseconds int64
	Title              string
	Artist             string
	Album              string
}

// SearchLyrics finds recordings whose lyrics match the given query,
// ranked by FTS5 relevance.  The query is treated as a phrase so a
// fragment like "hello darkness my old friend" matches consecutive
// words rather than each word independently.  Returns nil for an
// empty query.
func (d *DB) SearchLyrics(query string, limit int) ([]LyricsHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	if limit <= 0 {
		limit = 25
	}

	ftsQuery := buildLyricsPhraseQuery(query)
	if ftsQuery == "" {
		return nil, nil
	}

	// lyrics_index.rowid is the audio file's id, so the hit is already
	// a playable file - it used to be a recording id, which then had to
	// be mapped back to "some file of that recording" by a grouped
	// subquery.
	//
	// SAFETY: FTS5 MATCH syntax unsupported by sqlc. Query is parameterized; no string interpolation.
	rows, err := d.reader().QueryContext(d.Ctx, `
		SELECT
			tm.id,
			tm.file_path,
			tm.length_milliseconds,
			tm.title,
			tm.artist_name,
			tm.album
		FROM lyrics_index li
		JOIN track_metadata tm ON tm.id = li.rowid
		WHERE lyrics_index MATCH ?
		ORDER BY rank
		LIMIT ?
	`, ftsQuery, limit)
	if err != nil {
		return nil, fmt.Errorf("lyrics search failed: %w", err)
	}

	defer func() { _ = rows.Close() }()

	var results []LyricsHit

	for rows.Next() {
		var h LyricsHit
		if err := rows.Scan(
			&h.AudioFileID,
			&h.FilePath,
			&h.LengthMilliseconds,
			&h.Title,
			&h.Artist,
			&h.Album,
		); err != nil {
			return nil, fmt.Errorf("could not scan lyrics hit: %w", err)
		}

		results = append(results, h)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("lyrics hit iteration error: %w", err)
	}

	return results, nil
}

// GetLyrics returns the stored lyrics for a file, or "" if none.
func (d *DB) GetLyrics(audioFileID int64) (string, error) {
	var lyrics string

	err := d.reader().QueryRowContext(d.Ctx,
		"SELECT text FROM lyrics WHERE audio_file_id = ?", audioFileID,
	).Scan(&lyrics)

	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}

	if err != nil {
		return "", fmt.Errorf("could not read lyrics: %w", err)
	}

	return lyrics, nil
}

// SetLyrics writes lyrics for a file and keeps the FTS index in sync.
//
// `source` says where they came from, which is the question the old
// column could not answer: lyrics read from a USLT frame are rebuilt
// free by any rescan, and lyrics fetched from LRCLIB are network
// traffic nobody wants to repeat.  Passing an empty string clears both
// the row and the index entry.
func (d *DB) SetLyrics(audioFileID int64, lyrics, source, recordingMBID string) error {
	if strings.TrimSpace(lyrics) == "" {
		if _, err := d.db.ExecContext(d.Ctx,
			"DELETE FROM lyrics WHERE audio_file_id = ?", audioFileID,
		); err != nil {
			return fmt.Errorf("could not delete lyrics: %w", err)
		}

		return d.upsertLyricsIndex(audioFileID, "")
	}

	if _, err := d.db.ExecContext(d.Ctx, `
		INSERT INTO lyrics (audio_file_id, text, source, recording_mbid)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(audio_file_id) DO UPDATE SET
			text = excluded.text,
			source = excluded.source,
			recording_mbid = COALESCE(excluded.recording_mbid, lyrics.recording_mbid),
			fetched_at = CURRENT_TIMESTAMP
	`, audioFileID, lyrics, source, toNullString(recordingMBID)); err != nil {
		return fmt.Errorf("could not write lyrics: %w", err)
	}

	return d.upsertLyricsIndex(audioFileID, lyrics)
}

// upsertLyricsIndex refreshes a single file's entry in the contentless
// lyrics_index.  contentless_delete=1 makes the DELETE valid; an empty
// lyrics string leaves the row deleted.
func (d *DB) upsertLyricsIndex(audioFileID int64, lyrics string) error {
	if _, err := d.db.ExecContext(d.Ctx,
		"DELETE FROM lyrics_index WHERE rowid = ?", audioFileID,
	); err != nil {
		return fmt.Errorf("could not delete lyrics_index row: %w", err)
	}

	if strings.TrimSpace(lyrics) == "" {
		return nil
	}

	// SAFETY: FTS5 virtual table INSERT unsupported by sqlc. All values parameterized.
	if _, err := d.db.ExecContext(d.Ctx,
		"INSERT INTO lyrics_index(rowid, lyrics) VALUES (?, ?)",
		audioFileID, lyrics,
	); err != nil {
		return fmt.Errorf("could not insert lyrics_index row: %w", err)
	}

	return nil
}

// RebuildLyricsIndex repopulates lyrics_index from the lyrics table.
func (d *DB) RebuildLyricsIndex() error {
	if _, err := d.db.ExecContext(d.Ctx, "DELETE FROM lyrics_index"); err != nil {
		return fmt.Errorf("could not clear lyrics_index: %w", err)
	}

	// SAFETY: FTS5 virtual table INSERT. Values sourced from lyrics; no user input.
	if _, err := d.db.ExecContext(d.Ctx, `
		INSERT INTO lyrics_index(rowid, lyrics)
		SELECT audio_file_id, text FROM lyrics WHERE text != ''
	`); err != nil {
		return fmt.Errorf("could not rebuild lyrics_index: %w", err)
	}

	return nil
}

// LyricsCandidate identifies a file that needs its lyrics fetched and
// carries the fields an external provider matches on.
type LyricsCandidate struct {
	AudioFileID        int64
	Title              string
	Artist             string
	Album              string
	RecordingMBID      string
	LengthMilliseconds int64
}

// FilesMissingLyrics returns files with no stored lyrics that carry
// the artist/title/duration needed to look them up.  Used by the
// LRCLIB backfill; the limit bounds each batch.
func (d *DB) FilesMissingLyrics(limit int) ([]LyricsCandidate, error) {
	if limit <= 0 {
		limit = 200
	}

	rows, err := d.reader().QueryContext(d.Ctx, `
		SELECT tm.id, tm.title, tm.artist_name, tm.album,
		       tm.recording_mbid, tm.length_milliseconds
		FROM track_metadata tm
		WHERE NOT EXISTS (SELECT 1 FROM lyrics l WHERE l.audio_file_id = tm.id)
		  AND tm.title != '' AND tm.artist_name != ''
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("could not query files missing lyrics: %w", err)
	}

	defer func() { _ = rows.Close() }()

	var out []LyricsCandidate

	for rows.Next() {
		var c LyricsCandidate
		if err := rows.Scan(
			&c.AudioFileID, &c.Title, &c.Artist, &c.Album,
			&c.RecordingMBID, &c.LengthMilliseconds,
		); err != nil {
			return nil, fmt.Errorf("could not scan lyrics candidate: %w", err)
		}

		out = append(out, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("lyrics candidate iteration error: %w", err)
	}

	return out, nil
}

// FileLyricLookup returns the provider-match fields for one file, so
// lyrics can be fetched on demand.  Returns nil if the file has no
// artist/title to match on.
func (d *DB) FileLyricLookup(audioFileID int64) (*LyricsCandidate, error) {
	var c LyricsCandidate

	err := d.reader().QueryRowContext(d.Ctx, `
		SELECT tm.id, tm.title, tm.artist_name, tm.album,
		       tm.recording_mbid, tm.length_milliseconds
		FROM track_metadata tm
		WHERE tm.id = ?
	`, audioFileID).Scan(
		&c.AudioFileID, &c.Title, &c.Artist, &c.Album,
		&c.RecordingMBID, &c.LengthMilliseconds,
	)
	if err != nil {
		return nil, fmt.Errorf("could not look up file for lyrics: %w", err)
	}

	if c.Title == "" || c.Artist == "" {
		return nil, nil
	}

	return &c, nil
}

// buildLyricsPhraseQuery turns a user's lyric fragment into an FTS5
// phrase query — a single double-quoted string of tokens — so the
// words must appear adjacently ("hello darkness my old friend"),
// which is what a lyric search means.  All non-alphanumeric runes are
// treated as separators (matching the unicode61 tokeniser), so no
// user character can break out of the quoted phrase.  Returns "" when
// the fragment has no searchable tokens.
func buildLyricsPhraseQuery(query string) string {
	fields := strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	if len(fields) == 0 {
		return ""
	}

	return `"` + strings.Join(fields, " ") + `"`
}
