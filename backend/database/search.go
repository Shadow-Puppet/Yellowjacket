// Package database provides SQLite database access.
package database

import (
	"database/sql"
	"fmt"
	"strings"
)

// SearchRow holds a single result from an FTS5 or basename search.
type SearchRow struct {
	FilePath           string
	LengthMilliseconds int64
	Title              string
	Artist             string
	Album              string
}

// SearchFTS performs a full-text search across title, artist, album,
// and file_path using the FTS5 search_index.  The query string is
// tokenised by FTS5's unicode61 tokeniser.
func (d *DB) SearchFTS(
	query string, limit int,
) ([]SearchRow, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	// Escape double quotes and wrap each token in quotes so
	// special characters are treated as literals.
	ftsQuery := buildFTSQuery(query)

	// SAFETY: FTS5 MATCH syntax unsupported by sqlc. Query is parameterized; no string interpolation.
	rows, err := d.db.QueryContext(d.Ctx, `
		SELECT
			tm.file_path,
			tm.length_milliseconds,
			tm.title,
			tm.artist_name,
			tm.album
		FROM search_index si
		JOIN track_metadata tm ON tm.id = si.rowid
		WHERE search_index MATCH ?
		ORDER BY rank
		LIMIT ?
	`, ftsQuery, limit)
	if err != nil {
		return nil, fmt.Errorf(
			"FTS search failed: %w", err,
		)
	}

	defer func() { _ = rows.Close() }()

	return scanSearchRows(rows)
}

// SearchFTSByFilename searches the file_path column of the FTS5
// index for tokens extracted from the given basename.
func (d *DB) SearchFTSByFilename(
	basename string, limit int,
) ([]SearchRow, error) {
	basename = strings.TrimSpace(basename)
	if basename == "" {
		return nil, nil
	}

	// Strip extension and build an FTS query scoped to
	// the file_path column.
	stem := stripExtForSearch(basename)
	tokens := tokeniseForFTS(stem)

	if len(tokens) == 0 {
		return nil, nil
	}

	ftsQuery := "file_path : " +
		strings.Join(tokens, " ")

	// SAFETY: FTS5 MATCH syntax unsupported by sqlc. Query is parameterized; no string interpolation.
	rows, err := d.db.QueryContext(d.Ctx, `
		SELECT
			tm.file_path,
			tm.length_milliseconds,
			tm.title,
			tm.artist_name,
			tm.album
		FROM search_index si
		JOIN track_metadata tm ON tm.id = si.rowid
		WHERE search_index MATCH ?
		ORDER BY rank
		LIMIT ?
	`, ftsQuery, limit)
	if err != nil {
		return nil, fmt.Errorf(
			"FTS filename search failed: %w", err,
		)
	}

	defer func() { _ = rows.Close() }()

	return scanSearchRows(rows)
}

// InsertSearchIndex adds a row to the FTS5 search_index.
func (d *DB) InsertSearchIndex(
	rowid int64,
	filePath, title, artist, album string,
) error {
	// SAFETY: FTS5 virtual table INSERT unsupported by sqlc. All values are parameterized.
	_, err := d.db.ExecContext(d.Ctx, `
		INSERT INTO search_index(rowid, file_path, title, artist, album)
		VALUES (?, ?, ?, ?, ?)
	`, rowid, filePath, title, artist, album)

	return err
}

// DeleteSearchIndex removes a single row from the FTS5 search_index
// by rowid.  This is supported because the table uses
// contentless_delete=1.  Phase 16 uses this for inline tag edits
// (delete old entry, reinsert with updated metadata).
func (d *DB) DeleteSearchIndex(rowid int64) error {
	_, err := d.db.ExecContext(d.Ctx,
		`DELETE FROM search_index WHERE rowid = ?`, rowid,
	)
	if err != nil {
		return fmt.Errorf("could not delete search index entry: %w", err)
	}

	return nil
}

// ClearSearchIndex removes all rows from the FTS5 search_index.
// We drop and recreate it to ensure a clean slate for full rebuilds.
func (d *DB) ClearSearchIndex() error {
	// SAFETY: Drop + recreate for full rebuild.  No parameters.
	if _, err := d.db.ExecContext(d.Ctx,
		`DROP TABLE IF EXISTS search_index`,
	); err != nil {
		return fmt.Errorf("could not drop search_index: %w", err)
	}

	if _, err := d.db.ExecContext(d.Ctx, `
		CREATE VIRTUAL TABLE IF NOT EXISTS search_index USING fts5(
			file_path,
			title,
			artist,
			album,
			content='',
			contentless_delete=1,
			tokenize='unicode61 remove_diacritics 2'
		)
	`); err != nil {
		return fmt.Errorf("could not recreate search_index: %w", err)
	}

	return nil
}

// RebuildSearchIndex repopulates the FTS5 search_index from
// scratch using current audio_files + recordings data.
func (d *DB) RebuildSearchIndex() error {
	if err := d.ClearSearchIndex(); err != nil {
		return fmt.Errorf(
			"could not clear search index: %w", err,
		)
	}

	// SAFETY: FTS5 virtual table INSERT unsupported by sqlc. All values sourced from track_metadata VIEW; no user input.
	_, err := d.db.ExecContext(d.Ctx, `
		INSERT INTO search_index(rowid, file_path, title, artist, album)
		SELECT id, file_path, title, artist_name, album
		FROM track_metadata
	`)
	if err != nil {
		return fmt.Errorf(
			"could not rebuild search index: %w", err,
		)
	}

	return nil
}

// SearchTrackRow holds a full track result from an FTS5 search,
// matching all 16 columns returned by GetAllTracksWithFullMetadata.
type SearchTrackRow struct {
	FilePath           string
	LengthMilliseconds int64
	Title              string
	ArtistName         string
	TrackNumber        sql.NullInt64
	DiscNumber         sql.NullInt64
	Album              string
	Genre              string
	Year               int64
	Composer           string
	FileType           string
	SampleRate         int64
	BitDepth           int64
	Channels           int64
	Bitrate            int64
	FileSize           int64
}

// SearchFTSTracks performs a full-text search and returns full track
// metadata for each match.  Unlike SearchFTS (which returns only 5
// columns), this includes all 16 fields needed for library.Track.
func (d *DB) SearchFTSTracks(
	query string, limit int,
) ([]SearchTrackRow, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	ftsQuery := buildFTSQuery(query)

	// SAFETY: FTS5 MATCH syntax unsupported by sqlc. Query is parameterized; no string interpolation.
	rows, err := d.db.QueryContext(d.Ctx, `
		SELECT
			tm.file_path,
			tm.length_milliseconds,
			tm.title,
			tm.artist_name,
			tm.track_number,
			tm.disc_number,
			tm.album,
			tm.genre,
			tm.year,
			tm.composer,
			tm.file_type,
			tm.sample_rate,
			tm.bit_depth,
			tm.channels,
			tm.bitrate,
			tm.file_size
		FROM search_index si
		JOIN track_metadata tm ON tm.id = si.rowid
		WHERE search_index MATCH ?
		ORDER BY rank
		LIMIT ?
	`, ftsQuery, limit)
	if err != nil {
		return nil, fmt.Errorf(
			"FTS track search failed: %w", err,
		)
	}

	defer func() { _ = rows.Close() }()

	var results []SearchTrackRow

	for rows.Next() {
		var r SearchTrackRow

		if err := rows.Scan(
			&r.FilePath,
			&r.LengthMilliseconds,
			&r.Title,
			&r.ArtistName,
			&r.TrackNumber,
			&r.DiscNumber,
			&r.Album,
			&r.Genre,
			&r.Year,
			&r.Composer,
			&r.FileType,
			&r.SampleRate,
			&r.BitDepth,
			&r.Channels,
			&r.Bitrate,
			&r.FileSize,
		); err != nil {
			return nil, fmt.Errorf(
				"could not scan search track row: %w",
				err,
			)
		}

		results = append(results, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"search track row iteration error: %w",
			err,
		)
	}

	return results, nil
}

// SearchFTSTracksByLibrary performs a full-text search scoped to a
// specific library and returns full track metadata for each match.
func (d *DB) SearchFTSTracksByLibrary(
	query string, limit int, libraryID int64,
) ([]SearchTrackRow, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	ftsQuery := buildFTSQuery(query)

	// SAFETY: FTS5 MATCH syntax unsupported by sqlc. Query is parameterized; no string interpolation.
	rows, err := d.db.QueryContext(d.Ctx, `
		SELECT
			tm.file_path,
			tm.length_milliseconds,
			tm.title,
			tm.artist_name,
			tm.track_number,
			tm.disc_number,
			tm.album,
			tm.genre,
			tm.year,
			tm.composer,
			tm.file_type,
			tm.sample_rate,
			tm.bit_depth,
			tm.channels,
			tm.bitrate,
			tm.file_size
		FROM search_index si
		JOIN track_metadata tm ON tm.id = si.rowid
		WHERE search_index MATCH ? AND tm.library_id = ?
		ORDER BY rank
		LIMIT ?
	`, ftsQuery, libraryID, limit)
	if err != nil {
		return nil, fmt.Errorf(
			"FTS library track search failed: %w", err,
		)
	}

	defer func() { _ = rows.Close() }()

	var results []SearchTrackRow

	for rows.Next() {
		var r SearchTrackRow

		if err := rows.Scan(
			&r.FilePath,
			&r.LengthMilliseconds,
			&r.Title,
			&r.ArtistName,
			&r.TrackNumber,
			&r.DiscNumber,
			&r.Album,
			&r.Genre,
			&r.Year,
			&r.Composer,
			&r.FileType,
			&r.SampleRate,
			&r.BitDepth,
			&r.Channels,
			&r.Bitrate,
			&r.FileSize,
		); err != nil {
			return nil, fmt.Errorf(
				"could not scan library search track row: %w",
				err,
			)
		}

		results = append(results, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"library search track row iteration error: %w",
			err,
		)
	}

	return results, nil
}

// scanSearchRows reads all rows from a query result into a slice.
func scanSearchRows(
	rows interface {
		Next() bool
		Scan(dest ...any) error
		Err() error
	},
) ([]SearchRow, error) {
	var results []SearchRow

	for rows.Next() {
		var r SearchRow

		if err := rows.Scan(
			&r.FilePath,
			&r.LengthMilliseconds,
			&r.Title,
			&r.Artist,
			&r.Album,
		); err != nil {
			return nil, fmt.Errorf(
				"could not scan search row: %w", err,
			)
		}

		results = append(results, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"search row iteration error: %w", err,
		)
	}

	return results, nil
}

// buildFTSQuery converts a user query string into an FTS5 query.
// Each word is quoted to escape special characters and combined
// with implicit AND.
func buildFTSQuery(query string) string {
	tokens := tokeniseForFTS(query)
	if len(tokens) == 0 {
		return query
	}

	return strings.Join(tokens, " ")
}

// tokeniseForFTS splits a string on whitespace and common
// separators, returning quoted FTS5 tokens.
func tokeniseForFTS(s string) []string {
	// Split on whitespace, hyphens, underscores, dots.
	fields := strings.FieldsFunc(
		s, func(r rune) bool {
			return r == ' ' || r == '-' ||
				r == '_' || r == '.' ||
				r == '/' || r == '\\'
		},
	)

	tokens := make([]string, 0, len(fields))

	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}

		// Escape any double quotes inside the token.
		f = strings.ReplaceAll(f, `"`, `""`)
		tokens = append(tokens, `"`+f+`"`)
	}

	return tokens
}

// stripExtForSearch removes the file extension from a string.
func stripExtForSearch(s string) string {
	if idx := strings.LastIndexByte(s, '.'); idx > 0 {
		return s[:idx]
	}

	return s
}
