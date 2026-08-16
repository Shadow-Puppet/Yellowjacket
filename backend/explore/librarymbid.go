package explore

import (
	"strings"

	"yellowjacket/backend/database"
)

// LibraryMBIDIndex provides fast MBID lookups against the local
// music library.  Used for "In Library" badges on explore search
// results and for sharing artist images with local views.
type LibraryMBIDIndex struct {
	db *database.DB
}

// NewLibraryMBIDIndex creates a library MBID lookup service.
func NewLibraryMBIDIndex(db *database.DB) *LibraryMBIDIndex {
	return &LibraryMBIDIndex{db: db}
}

// CheckMBIDs returns which of the given MBIDs the library actually
// has a file for.  The returned map is MBID -> entity type ("artist",
// "release_group", or "recording").
//
// The "has a file" part is the whole point and is what this used to get
// wrong.  It was three `SELECT mbid FROM <metadata table>` queries, and
// a metadata row could outlive the file that created it - retagging a
// file abandoned its old recording row, which kept the old MBID
// forever.  Measured on a real library: 812 orphaned recordings, of
// which 218 carried MBIDs, and 129 catalog rows that this function
// therefore reported as owned.  Every one of them rendered as a track
// you have, with a play button that could not work, because playback
// resolves files and this resolved metadata.
//
// Each branch now joins audio_files.  An entity is in your library if
// and only if a file says so.
func (idx *LibraryMBIDIndex) CheckMBIDs(mbids []string) map[string]string {
	if len(mbids) == 0 {
		return nil
	}

	result := make(map[string]string, len(mbids))

	type entityQuery struct {
		entityType string
		query      string
	}

	queries := []entityQuery{
		{"recording", `SELECT DISTINCT recording_mbid FROM audio_files
			WHERE recording_mbid IN (%s)`},
		{"release_group", `SELECT DISTINCT al.mbid FROM albums al
			JOIN audio_files af ON af.album_id = al.id
			WHERE al.mbid IN (%s)`},
		{"artist", `SELECT DISTINCT a.mbid FROM artists a
			WHERE a.mbid IN (%s) AND (
				EXISTS (SELECT 1 FROM audio_files af WHERE af.artist_id = a.id)
				OR EXISTS (
					SELECT 1 FROM albums al
					JOIN audio_files af2 ON af2.album_id = al.id
					WHERE al.artist_id = a.id
				)
			)`},
	}

	// Build a set of MBIDs still unresolved.
	remaining := make(map[string]bool, len(mbids))
	for _, m := range mbids {
		if m != "" {
			remaining[m] = true
		}
	}

	for _, eq := range queries {
		if len(remaining) == 0 {
			break
		}

		placeholders := make([]string, 0, len(remaining))
		args := make([]any, 0, len(remaining))

		for m := range remaining {
			placeholders = append(placeholders, "?")
			args = append(args, m)
		}

		//nolint:gosec // the query text is a constant from the slice above
		query := strings.Replace(
			eq.query, "%s", strings.Join(placeholders, ","), 1,
		)

		rows, err := idx.db.QueryContext(query, args...)
		if err != nil {
			continue
		}

		for rows.Next() {
			var mbid string
			if err := rows.Scan(&mbid); err == nil {
				result[mbid] = eq.entityType
				delete(remaining, mbid)
			}
		}

		_ = rows.Close()
	}

	return result
}

// GetArtistMBID returns the MBID for a local artist by name, or "".
func (idx *LibraryMBIDIndex) GetArtistMBID(artistName string) string {
	rows, err := idx.db.QueryContext(
		"SELECT mbid FROM artists WHERE name = ? AND mbid IS NOT NULL LIMIT 1",
		artistName,
	)
	if err != nil {
		return ""
	}

	defer func() { _ = rows.Close() }()

	if rows.Next() {
		var mbid string
		if err := rows.Scan(&mbid); err == nil {
			return mbid
		}
	}

	return ""
}

// AllArtistMBIDs returns all (name, mbid) pairs for artists that
// have MBIDs.  Used by the search index Tier 3 for direct matching.
func (idx *LibraryMBIDIndex) AllArtistMBIDs() map[string]string {
	rows, err := idx.db.QueryContext(
		"SELECT name, mbid FROM artists WHERE mbid IS NOT NULL AND mbid != ''",
	)
	if err != nil {
		return nil
	}

	defer func() { _ = rows.Close() }()

	result := make(map[string]string)

	for rows.Next() {
		var name, mbid string
		if err := rows.Scan(&name, &mbid); err == nil {
			result[name] = mbid
		}
	}

	return result
}

//nolint:unused // utility kept for future per-MBID existence checks.
func (idx *LibraryMBIDIndex) exists(table, mbid string) bool {
	//nolint:gosec // table name is hardcoded from internal callers only
	rows, err := idx.db.QueryContext(
		"SELECT 1 FROM "+table+" WHERE mbid = ? LIMIT 1",
		mbid,
	)
	if err != nil {
		return false
	}

	defer func() { _ = rows.Close() }()

	return rows.Next()
}
