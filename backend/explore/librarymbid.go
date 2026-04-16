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

// CheckMBIDs returns which of the given MBIDs exist in the local
// library.  The returned map has MBID → entity type ("artist",
// "release_group", or "recording").
func (idx *LibraryMBIDIndex) CheckMBIDs(mbids []string) map[string]string {
	if len(mbids) == 0 {
		return nil
	}

	result := make(map[string]string, len(mbids))

	// Batch check all MBIDs against each table with a single IN query.
	type tableEntity struct {
		table      string
		entityType string
	}

	tables := []tableEntity{
		{"artists", "artist"},
		{"release_groups", "release_group"},
		{"recordings", "recording"},
	}

	// Build a set of MBIDs still unresolved.
	remaining := make(map[string]bool, len(mbids))
	for _, m := range mbids {
		if m != "" {
			remaining[m] = true
		}
	}

	for _, te := range tables {
		if len(remaining) == 0 {
			break
		}

		// Build IN clause from remaining MBIDs.
		placeholders := make([]string, 0, len(remaining))
		args := make([]any, 0, len(remaining))

		for m := range remaining {
			placeholders = append(placeholders, "?")
			args = append(args, m)
		}

		//nolint:gosec // table name is hardcoded from the tables slice above
		query := "SELECT mbid FROM " + te.table + " WHERE mbid IN (" +
			strings.Join(placeholders, ",") + ")"

		rows, err := idx.db.QueryContext(query, args...)
		if err != nil {
			continue
		}

		for rows.Next() {
			var mbid string
			if err := rows.Scan(&mbid); err == nil {
				result[mbid] = te.entityType
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
