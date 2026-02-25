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

	rows, err := d.db.QueryContext(d.Ctx, `
		SELECT
			af.file_path,
			af.length_milliseconds,
			COALESCE(r.name, ''),
			COALESCE(ac.text, ''),
			COALESCE(rg.name, '')
		FROM search_index si
		JOIN audio_files af ON af.id = si.rowid
		LEFT JOIN recordings r
			ON af.recording_id = r.id
		LEFT JOIN artist_credit ac
			ON r.artist_credit_id = ac.id
		LEFT JOIN (
			SELECT recording_id,
				MIN(release_group_id) AS release_group_id
			FROM release_group_recordings
			GROUP BY recording_id
		) rgr ON r.id = rgr.recording_id
		LEFT JOIN release_groups rg
			ON rgr.release_group_id = rg.id
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

	rows, err := d.db.QueryContext(d.Ctx, `
		SELECT
			af.file_path,
			af.length_milliseconds,
			COALESCE(r.name, ''),
			COALESCE(ac.text, ''),
			COALESCE(rg.name, '')
		FROM search_index si
		JOIN audio_files af ON af.id = si.rowid
		LEFT JOIN recordings r
			ON af.recording_id = r.id
		LEFT JOIN artist_credit ac
			ON r.artist_credit_id = ac.id
		LEFT JOIN (
			SELECT recording_id,
				MIN(release_group_id) AS release_group_id
			FROM release_group_recordings
			GROUP BY recording_id
		) rgr ON r.id = rgr.recording_id
		LEFT JOIN release_groups rg
			ON rgr.release_group_id = rg.id
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
	_, err := d.db.ExecContext(d.Ctx, `
		INSERT INTO search_index(rowid, file_path, title, artist, album)
		VALUES (?, ?, ?, ?, ?)
	`, rowid, filePath, title, artist, album)

	return err
}

// DeleteSearchIndex removes a row from the FTS5 search_index.
func (d *DB) DeleteSearchIndex(rowid int64) error {
	_, err := d.db.ExecContext(d.Ctx, `
		DELETE FROM search_index WHERE rowid = ?
	`, rowid)

	return err
}

// ClearSearchIndex removes all rows from the FTS5 search_index.
func (d *DB) ClearSearchIndex() error {
	_, err := d.db.ExecContext(d.Ctx, `
		DELETE FROM search_index
	`)

	return err
}

// RebuildSearchIndex repopulates the FTS5 search_index from
// scratch using current audio_files + recordings data.
func (d *DB) RebuildSearchIndex() error {
	if err := d.ClearSearchIndex(); err != nil {
		return fmt.Errorf(
			"could not clear search index: %w", err,
		)
	}

	_, err := d.db.ExecContext(d.Ctx, `
		INSERT INTO search_index(rowid, file_path, title, artist, album)
		SELECT
			af.id,
			af.file_path,
			COALESCE(r.name, ''),
			COALESCE(ac.text, ''),
			COALESCE(rg.name, '')
		FROM audio_files af
		LEFT JOIN recordings r ON af.recording_id = r.id
		LEFT JOIN artist_credit ac
			ON r.artist_credit_id = ac.id
		LEFT JOIN (
			SELECT recording_id,
				MIN(release_group_id) AS release_group_id
			FROM release_group_recordings
			GROUP BY recording_id
		) rgr ON r.id = rgr.recording_id
		LEFT JOIN release_groups rg
			ON rgr.release_group_id = rg.id
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

	rows, err := d.db.QueryContext(d.Ctx, `
		SELECT
			af.file_path,
			af.length_milliseconds,
			COALESCE(r.name, '')              AS title,
			COALESCE(ac.text, '')             AS artist_name,
			r.track_number,
			r.disc_number,
			COALESCE(rg.name, '')             AS album,
			CAST(COALESCE(
				(SELECT GROUP_CONCAT(g.name, '||')
				 FROM recording_genres rg_sub
				 JOIN genres g ON rg_sub.genre_id = g.id
				 WHERE rg_sub.recording_id = r.id),
				''
			) AS TEXT)                        AS genre,
			COALESCE(r.year, 0)               AS year,
			COALESCE(r.composer, '')           AS composer,
			COALESCE(ft.extension, '')         AS file_type,
			af.sample_rate,
			af.bit_depth,
			af.channels,
			af.bitrate,
			af.file_size
		FROM search_index si
		JOIN audio_files af ON af.id = si.rowid
		LEFT JOIN recordings r
			ON af.recording_id = r.id
		LEFT JOIN artist_credit ac
			ON r.artist_credit_id = ac.id
		LEFT JOIN (
			SELECT recording_id,
				MIN(release_group_id) AS release_group_id
			FROM release_group_recordings
			GROUP BY recording_id
		) rgr ON r.id = rgr.recording_id
		LEFT JOIN release_groups rg
			ON rgr.release_group_id = rg.id
		LEFT JOIN file_types ft
			ON af.file_type_id = ft.id
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
