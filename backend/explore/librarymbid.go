package explore

import (
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

	// Check each table.  For a small number of MBIDs this is fine.
	// For bulk checks we'd use a temp table join, but search results
	// are capped at ~30 MBIDs total.
	for _, mbid := range mbids {
		if mbid == "" {
			continue
		}

		// Check artists.
		if idx.exists("artists", mbid) {
			result[mbid] = "artist"

			continue
		}

		// Check release groups.
		if idx.exists("release_groups", mbid) {
			result[mbid] = "release_group"

			continue
		}

		// Check recordings.
		if idx.exists("recordings", mbid) {
			result[mbid] = "recording"
		}
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
