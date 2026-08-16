// Package database provides SQLite database access.
package database

import (
	"database/sql"
	"fmt"
	"strings"

	"yellowjacket/backend/database/sql/sqlcgen"
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

// trackMetadataColumns is the column list of the track_metadata view,
// in the order sqlc generates TrackMetadatum's fields.  The FTS
// searches below cannot be sqlc queries (MATCH is not in its grammar),
// so this is the one place the view's shape is written out by hand.
const trackMetadataColumns = `
	tm.id, tm.file_path, tm.length_milliseconds, tm.title, tm.artist_name,
	tm.track_number, tm.disc_number, tm.album, tm.genre, tm.year,
	tm.release_year, tm.composer, tm.file_type, tm.sample_rate,
	tm.bit_depth, tm.channels, tm.bitrate, tm.file_size, tm.library_id,
	tm.play_count, tm.last_played, tm.cover_art_path, tm.artist_mbid,
	tm.release_group_mbid, tm.recording_mbid, tm.album_id, tm.artist_id`

// scanTrackMetadata reads track_metadata rows into the generated row
// type, so an FTS hit and an ordinary query produce the same Track.
func scanTrackMetadata(rows *sql.Rows) ([]sqlcgen.TrackMetadatum, error) {
	var out []sqlcgen.TrackMetadatum

	for rows.Next() {
		var r sqlcgen.TrackMetadatum

		if err := rows.Scan(
			&r.ID, &r.FilePath, &r.LengthMilliseconds, &r.Title, &r.ArtistName,
			&r.TrackNumber, &r.DiscNumber, &r.Album, &r.Genre, &r.Year,
			&r.ReleaseYear, &r.Composer, &r.FileType, &r.SampleRate,
			&r.BitDepth, &r.Channels, &r.Bitrate, &r.FileSize, &r.LibraryID,
			&r.PlayCount, &r.LastPlayed, &r.CoverArtPath, &r.ArtistMbid,
			&r.ReleaseGroupMbid, &r.RecordingMbid, &r.AlbumID, &r.ArtistID,
		); err != nil {
			return nil, fmt.Errorf("scan track metadata: %w", err)
		}

		out = append(out, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate track metadata: %w", err)
	}

	return out, nil
}

// SearchFTSTracks performs a full-text search and returns whole tracks.
//
// A library id of 0 means every library.  There were two of these, one
// per case, each with its own copy of a sixteen-column projection that
// silently dropped the MBIDs and the play count - which is why the
// caller used to pass zeros for them.
func (d *DB) SearchFTSTracks(
	query string, libraryID int64, limit int,
) ([]sqlcgen.TrackMetadatum, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	// SAFETY: FTS5 MATCH syntax unsupported by sqlc. Query is parameterized; no string interpolation.
	rows, err := d.reader().QueryContext(d.Ctx, `
		SELECT`+trackMetadataColumns+`
		FROM search_index si
		JOIN track_metadata tm ON tm.id = si.rowid
		WHERE search_index MATCH ?
		  AND (? = 0 OR tm.library_id = ?)
		ORDER BY rank
		LIMIT ?
	`, buildFTSQuery(query), libraryID, libraryID, limit)
	if err != nil {
		return nil, fmt.Errorf("FTS track search failed: %w", err)
	}

	defer func() { _ = rows.Close() }()

	return scanTrackMetadata(rows)
}

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
