package database

import (
	"fmt"
	"strings"
	"unicode"
)

// LyricsHit is a single result from a lyric-fragment search: the
// matched recording plus enough metadata to render and play it.
type LyricsHit struct {
	RecordingID        int64
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

	// Map the matched recording (lyrics_index.rowid == recordings.id)
	// to a representative playable file via the lowest audio_files id,
	// then to the track_metadata VIEW for display fields.
	//
	// SAFETY: FTS5 MATCH syntax unsupported by sqlc. Query is parameterized; no string interpolation.
	rows, err := d.db.QueryContext(d.Ctx, `
		SELECT
			r.id,
			tm.file_path,
			tm.length_milliseconds,
			tm.title,
			tm.artist_name,
			tm.album
		FROM lyrics_index li
		JOIN recordings r ON r.id = li.rowid
		JOIN (
			SELECT recording_id, MIN(id) AS af_id
			FROM audio_files
			GROUP BY recording_id
		) af ON af.recording_id = r.id
		JOIN track_metadata tm ON tm.id = af.af_id
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
			&h.RecordingID,
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

// GetRecordingLyrics returns the stored lyrics for a recording, or
// an empty string if none are stored.
func (d *DB) GetRecordingLyrics(recordingID int64) (string, error) {
	var lyrics string

	err := d.db.QueryRowContext(d.Ctx,
		"SELECT COALESCE(lyrics, '') FROM recordings WHERE id = ?",
		recordingID,
	).Scan(&lyrics)
	if err != nil {
		return "", fmt.Errorf("could not read recording lyrics: %w", err)
	}

	return lyrics, nil
}

// SetRecordingLyrics writes lyrics onto a recording and keeps the FTS
// lyrics_index in sync (delete + reinsert the single row).  Used by
// the LRCLIB backfill to persist fetched lyrics.  Passing an empty
// string clears both the column and the index entry.
func (d *DB) SetRecordingLyrics(recordingID int64, lyrics string) error {
	if _, err := d.db.ExecContext(d.Ctx,
		"UPDATE recordings SET lyrics = ? WHERE id = ?",
		lyrics, recordingID,
	); err != nil {
		return fmt.Errorf("could not update recording lyrics: %w", err)
	}

	return d.upsertLyricsIndex(recordingID, lyrics)
}

// upsertLyricsIndex refreshes a single recording's entry in the
// contentless lyrics_index.  contentless_delete=1 makes the DELETE
// valid; an empty lyrics string leaves the row deleted.
func (d *DB) upsertLyricsIndex(recordingID int64, lyrics string) error {
	if _, err := d.db.ExecContext(d.Ctx,
		"DELETE FROM lyrics_index WHERE rowid = ?", recordingID,
	); err != nil {
		return fmt.Errorf("could not delete lyrics_index row: %w", err)
	}

	if strings.TrimSpace(lyrics) == "" {
		return nil
	}

	// SAFETY: FTS5 virtual table INSERT unsupported by sqlc. All values parameterized.
	if _, err := d.db.ExecContext(d.Ctx,
		"INSERT INTO lyrics_index(rowid, lyrics) VALUES (?, ?)",
		recordingID, lyrics,
	); err != nil {
		return fmt.Errorf("could not insert lyrics_index row: %w", err)
	}

	return nil
}

// RebuildLyricsIndex repopulates lyrics_index from scratch using the
// current recordings table.  Cheap for a personal library and safe to
// run after every scan.
func (d *DB) RebuildLyricsIndex() error {
	if _, err := d.db.ExecContext(d.Ctx,
		"DELETE FROM lyrics_index",
	); err != nil {
		return fmt.Errorf("could not clear lyrics_index: %w", err)
	}

	// SAFETY: FTS5 virtual table INSERT unsupported by sqlc. Values sourced from recordings; no user input.
	if _, err := d.db.ExecContext(d.Ctx, `
		INSERT INTO lyrics_index(rowid, lyrics)
		SELECT id, lyrics
		FROM recordings
		WHERE lyrics IS NOT NULL AND lyrics != ''
	`); err != nil {
		return fmt.Errorf("could not rebuild lyrics_index: %w", err)
	}

	return nil
}

// RecordingsMissingLyrics returns recordings that have no stored
// lyrics but do carry the artist/title/duration needed to look them
// up from an external provider.  Used by the LRCLIB backfill.  The
// limit bounds each batch so the backfill can be run incrementally.
func (d *DB) RecordingsMissingLyrics(limit int) ([]LyricsCandidate, error) {
	if limit <= 0 {
		limit = 200
	}

	rows, err := d.db.QueryContext(d.Ctx, `
		SELECT
			r.id,
			COALESCE(r.name, ''),
			COALESCE(ac.text, ''),
			COALESCE(rg.name, ''),
			MIN(af.length_milliseconds)
		FROM recordings r
		JOIN audio_files af ON af.recording_id = r.id
		LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
		LEFT JOIN (
			SELECT recording_id, MIN(release_group_id) AS release_group_id
			FROM release_group_recordings
			GROUP BY recording_id
		) rgr ON rgr.recording_id = r.id
		LEFT JOIN release_groups rg ON rg.id = rgr.release_group_id
		WHERE (r.lyrics IS NULL OR r.lyrics = '')
		  AND r.name IS NOT NULL AND r.name != ''
		  AND ac.text IS NOT NULL AND ac.text != ''
		GROUP BY r.id
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("could not query recordings missing lyrics: %w", err)
	}

	defer func() { _ = rows.Close() }()

	var out []LyricsCandidate

	for rows.Next() {
		var c LyricsCandidate
		if err := rows.Scan(
			&c.RecordingID, &c.Title, &c.Artist, &c.Album, &c.LengthMilliseconds,
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

// LyricsCandidate identifies a recording that needs its lyrics fetched
// and carries the fields an external provider matches on.
type LyricsCandidate struct {
	RecordingID        int64
	Title              string
	Artist             string
	Album              string
	LengthMilliseconds int64
}

// RecordingLyricLookup returns the provider-match fields (artist,
// title, album, duration) for a single recording, so lyrics can be
// fetched on demand.  Returns nil if the recording has no audio file
// or no artist/title to match on.
func (d *DB) RecordingLyricLookup(recordingID int64) (*LyricsCandidate, error) {
	var c LyricsCandidate

	err := d.db.QueryRowContext(d.Ctx, `
		SELECT
			r.id,
			COALESCE(r.name, ''),
			COALESCE(ac.text, ''),
			COALESCE(rg.name, ''),
			COALESCE(MIN(af.length_milliseconds), 0)
		FROM recordings r
		JOIN audio_files af ON af.recording_id = r.id
		LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
		LEFT JOIN (
			SELECT recording_id, MIN(release_group_id) AS release_group_id
			FROM release_group_recordings
			GROUP BY recording_id
		) rgr ON rgr.recording_id = r.id
		LEFT JOIN release_groups rg ON rg.id = rgr.release_group_id
		WHERE r.id = ?
		GROUP BY r.id
	`, recordingID).Scan(&c.RecordingID, &c.Title, &c.Artist, &c.Album, &c.LengthMilliseconds)
	if err != nil {
		return nil, fmt.Errorf("could not look up recording for lyrics: %w", err)
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
