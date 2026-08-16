package explore

import (
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// The catalog stores a MusicBrainz id as its 16 raw bytes and an entity
// type as a small integer, rather than as the 36-character text and the
// words the rest of the app uses.
//
// This is a size decision and it is a large one. Measured on a real
// 2,052,200-row catalog: the three MBID columns and `entity_type` are
// 220 MB of a 383 MB table, and they are carried again in every index
// that keys on them. Converting the table and its four indexes took
// **677 MB to 389 MB** with the same row count.
//
// Everything above this file still speaks strings: `SearchIndexResult`
// carries `"artist"` and a dashed MBID, the frontend receives them, and
// the conversion happens only where a value crosses into SQL. The
// alternative - blobs and codes reaching the rest of the app - would
// trade 288 MB for a type that nothing else wants.
//
// Two things make a mistake here loud rather than silent, which matters
// because SQLite does not coerce between TEXT and BLOB: a query
// comparing a blob column against a string returns *no rows* rather
// than an error.
//
//   - Writes are guarded by a CHECK on the column (16 bytes, or empty),
//     so a stringly write fails at the insert rather than sitting in
//     the table looking fine.
//   - Reads scan into `dbMBID`, whose Scan rejects anything that is not
//     16 bytes or empty. A column that somehow holds text produces an
//     error instead of a garbled id.
type dbMBID string

// mbidLen is the byte length of a raw MusicBrainz id.
const mbidLen = 16

// Value encodes the id for storage: 16 raw bytes, or empty for "none".
func (m dbMBID) Value() (driver.Value, error) {
	return mbidBytes(string(m)), nil
}

// Scan decodes a stored id back to its canonical dashed form.
func (m *dbMBID) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*m = ""

		return nil
	case []byte:
		s, err := mbidFromBytes(v)
		if err != nil {
			return err
		}

		*m = dbMBID(s)

		return nil
	case string:
		// Tolerated for the one caller that reads through a view or a
		// literal: a canonical id is already the right answer.
		*m = dbMBID(v)

		return nil
	default:
		return fmt.Errorf("%w: %T", errMBIDType, src)
	}
}

// mbidBytes encodes a dashed MusicBrainz id as its 16 raw bytes.  An
// id that is not one - including the empty string, which is how "no
// MBID" is spelled throughout - encodes as empty.
func mbidBytes(s string) []byte {
	if s == "" {
		return []byte{}
	}

	raw, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	if err != nil || len(raw) != mbidLen {
		return []byte{}
	}

	return raw
}

// mbidFromBytes decodes stored bytes back to the canonical dashed form.
func mbidFromBytes(b []byte) (string, error) {
	if len(b) == 0 {
		return "", nil
	}

	if len(b) != mbidLen {
		return "", fmt.Errorf("%w: %d bytes", errMBIDLength, len(b))
	}

	h := hex.EncodeToString(b)

	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}

// Entity types are stored as codes.  The names are the app's, the codes
// are the table's, and nothing outside this file should see a code.
const (
	entityCodeArtist       = 1
	entityCodeReleaseGroup = 2
	entityCodeRecording    = 3
)

// Entity type names, as everything above the SQL boundary spells them.
const (
	EntityArtist       = "artist"
	EntityReleaseGroup = "release_group"
	EntityRecording    = "recording"
)

// entityCode maps a name to its stored code.  An unknown name yields 0,
// which matches no row - the same answer the old string comparison gave
// and the reason this is not an error.
func entityCode(name string) int {
	switch name {
	case EntityArtist:
		return entityCodeArtist
	case EntityReleaseGroup:
		return entityCodeReleaseGroup
	case EntityRecording:
		return entityCodeRecording
	default:
		return 0
	}
}

// entityName maps a stored code back to its name.
func entityName(code int) string {
	switch code {
	case entityCodeArtist:
		return EntityArtist
	case entityCodeReleaseGroup:
		return EntityReleaseGroup
	case entityCodeRecording:
		return EntityRecording
	default:
		return ""
	}
}

// dbEntityType scans a stored entity-type code as its name.
//
// The db prefix keeps it out of the way of the many `mbid string` and
// `entityType string` locals in this package: these two types exist
// only at the SQL boundary, and a name that shadowed one of those
// would turn a conversion into a confusing compile error rather than
// an obvious one.
type dbEntityType string

// Value encodes the name for storage.
func (e dbEntityType) Value() (driver.Value, error) {
	return int64(entityCode(string(e))), nil
}

// Scan decodes a stored code back to its name.
func (e *dbEntityType) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*e = ""

		return nil
	case int64:
		*e = dbEntityType(entityName(int(v)))

		return nil
	case string:
		*e = dbEntityType(v)

		return nil
	case []byte:
		*e = dbEntityType(v)

		return nil
	default:
		return fmt.Errorf("%w: %T", errEntityTypeType, src)
	}
}

// Errors from the encoding boundary.  They exist so a type confusion
// here is reported rather than silently producing an id that matches
// nothing.
var (
	errMBIDType       = errors.New("cannot scan MusicBrainz id from")
	errMBIDLength     = errors.New("stored MusicBrainz id has the wrong length")
	errEntityTypeType = errors.New("cannot scan entity type from")
)

// A query that names an entity type inline writes the code with the
// name beside it - `entity_type = 1 /* artist */`.  Splicing a Go
// constant into the SQL would keep them in step automatically but makes
// every such query a concatenation; the codes are pinned by
// TestEntityCodesAreStable instead, because they are a storage format
// and changing one is not a refactor.
