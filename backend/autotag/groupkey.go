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
// where the parent directory is lower-cased and disc_number is
// normalized so an untagged disc (0) folds into disc 1 — see
// normalizeDiscNumber.  The folder is taken as the album boundary —
// including the album tag string would fragment albums whose tracks
// carry slightly different tags (`Abbey Road` vs `Abbey Road
// (Remastered 2009)`, etc.).  The album name is still surfaced in
// `tagging_items.album_name` for the review UI; it just doesn't
// decide grouping.
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
	h.Write([]byte(strconv.Itoa(normalizeDiscNumber(discNumber))))

	return hex.EncodeToString(h.Sum(nil))
}

// SyntheticGroupKey returns a deterministic identifier for a
// tag-clustered sub-group carved out of parentGroupKey by
// SplitMixedFolder — same SHA-1-over-null-separated-fields shape as
// GroupKey, but keyed on the cluster's (album, album-artist) tags
// instead of a directory, since a synthetic group's tracks don't
// share a directory boundary distinct from their siblings left
// behind in the parent folder.
func SyntheticGroupKey(parentGroupKey, albumName, albumArtist string) string {
	h := sha1.New() //nolint:gosec // see package doc — grouping only.
	h.Write([]byte(parentGroupKey))
	h.Write([]byte{0})
	h.Write([]byte(Normalize(albumName)))
	h.Write([]byte{0})
	h.Write([]byte(Normalize(albumArtist)))

	return hex.EncodeToString(h.Sum(nil))
}

// SyntheticTrackGroupKey returns a deterministic identifier for a
// single leftover track carved out of a mixed-bag folder by
// SplitMixedFolder's singleton fallback (autotag.SplitPlan). Keyed on
// the track's own audio_files id rather than its tags — two
// untagged leftover tracks would otherwise both normalize to the
// same empty (album, album-artist) pair and collide under
// SyntheticGroupKey.
func SyntheticTrackGroupKey(parentGroupKey string, audioFileID int64) string {
	h := sha1.New() //nolint:gosec // see package doc — grouping only.
	h.Write([]byte(parentGroupKey))
	h.Write([]byte{0})
	h.Write([]byte("track"))
	h.Write([]byte{0})
	h.Write([]byte(strconv.FormatInt(audioFileID, 10)))

	return hex.EncodeToString(h.Sum(nil))
}

// normalizeDiscNumber folds a missing/invalid disc tag (<= 0) into
// disc 1 for grouping purposes.  Without this, a folder where only
// some tracks carry an explicit "disc 1 of 1" tag — common when
// files were ripped or re-tagged at different times — splits into
// two tagging groups for what is really one single-disc album: the
// untagged tracks hash to disc 0, the tagged ones to disc 1.  A
// genuine multi-disc release still separates correctly, since its
// disc-2-and-up tracks carry an explicit non-zero, non-one disc
// number.
func normalizeDiscNumber(discNumber int) int {
	if discNumber <= 0 {
		return 1
	}

	return discNumber
}
