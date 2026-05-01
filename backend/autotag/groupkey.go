// Package autotag provides the shared primitives used by the
// MusicBrainz autotagger: deterministic album-group keys, scoring,
// and (in later phases) MB orchestration.
package autotag

import (
	"crypto/sha1" //nolint:gosec // non-crypto deterministic grouping key.
	"encoding/hex"
	"path/filepath"
	"strconv"
	"strings"
)

// GroupKey returns a deterministic album-group identifier for the
// file at filePath belonging to libraryID, carrying the given disc
// number.
//
// The key is a lower-case hex SHA-1 over
//
//	libraryID || 0 || normalized_parent_dir || 0 || disc_number
//
// where the parent directory is lower-cased.  The folder is taken
// as the album boundary — including the album tag string would
// fragment albums whose tracks carry slightly different tags
// (`Abbey Road` vs `Abbey Road (Remastered 2009)`, etc.).  The
// album name is still surfaced in `tagging_items.album_name` for
// the review UI; it just doesn't decide grouping.
//
// Using SHA-1 matches the codebase's existing non-crypto
// deterministic-key convention; collision risk at album-group
// cardinality is irrelevant.
func GroupKey(
	libraryID int64,
	filePath string,
	discNumber int,
) string {
	parentDir := strings.ToLower(filepath.Dir(filePath))

	h := sha1.New() //nolint:gosec // see package doc — grouping only.
	h.Write([]byte(strconv.FormatInt(libraryID, 10)))
	h.Write([]byte{0})
	h.Write([]byte(parentDir))
	h.Write([]byte{0})
	h.Write([]byte(strconv.Itoa(discNumber)))

	return hex.EncodeToString(h.Sum(nil))
}
