package explore

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/parquet-go/parquet-go"
)

// Plumbing shared between the client and the CI-only index builder.
//
// The full dump import (dumpimport.go and friends) is behind the
// `indexbuild` build tag so it is not linked into the app: a user's
// machine never streams the ~89GB listens dump, it merges the prebuilt
// artifact instead.  What stays here is what the client genuinely still
// needs — the daily incremental refresh (dumpincremental.go) and the
// artifact download (artifactfetch.go) — plus the small helpers both
// sides share.

// mbidKey is a parsed UUID plus an entity-kind tag, used as the counts
// map key.  17 bytes instead of a 36-byte string keeps the ~40M-entry
// map around 2GB.
type mbidKey [17]byte

func makeMBIDKey(kind byte, mbid string) (mbidKey, bool) {
	var k mbidKey

	k[0] = kind

	if !parseUUID(mbid, k[1:]) {
		return k, false
	}

	return k, true
}

func formatUUID(b []byte) string {
	const hexdigits = "0123456789abcdef"

	out := make([]byte, 36)
	j := 0

	for i := range 16 {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			out[j] = '-'
			j++
		}

		out[j] = hexdigits[b[i]>>4]
		out[j+1] = hexdigits[b[i]&0x0F]
		j += 2
	}

	return string(out)
}

func formatGB(n int64) string {
	return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
}

// formatCount renders a number with thousands separators, matching how
// the frontend shows counts.
func formatCount(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}

	var b strings.Builder

	lead := len(s) % 3
	if lead > 0 {
		b.WriteString(s[:lead])
	}

	for i := lead; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}

		b.WriteString(s[i : i+3])
	}

	return b.String()
}

// parseListenParquet decodes one parquet member and returns the
// per-entity listen-count deltas.
func parseListenParquet(buf []byte) (map[mbidKey]uint32, error) {
	reader := parquet.NewGenericReader[sparkListenRow](bytes.NewReader(buf))

	defer func() { _ = reader.Close() }()

	deltas := make(map[mbidKey]uint32, 1<<18)
	rows := make([]sparkListenRow, 4096)

	for {
		n, err := reader.Read(rows)

		for _, row := range rows[:n] {
			key, ok := makeMBIDKey(countKindRecording, row.RecordingMBID)
			if !ok {
				// Unmapped listen — no usable recording MBID.
				continue
			}

			deltas[key]++

			if relKey, relOK := makeMBIDKey(countKindRelease, row.ReleaseMBID); relOK {
				deltas[relKey]++
			}

			for _, artist := range row.ArtistMBIDs {
				if artKey, artOK := makeMBIDKey(countKindArtist, artist); artOK {
					deltas[artKey]++
				}
			}
		}

		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("parquet read: %w", err)
		}

		if n == 0 {
			break
		}
	}

	return deltas, nil
}

// sparkListenRow is the projection of the spark listens parquet schema
// that the aggregator reads.  All other columns are skipped.
type sparkListenRow struct {
	RecordingMBID string   `parquet:"recording_mbid,optional"`
	ReleaseMBID   string   `parquet:"release_mbid,optional"`
	ArtistMBIDs   []string `parquet:"artist_credit_mbids,optional,list"`
}

// checkFreeDisk returns ErrDiskSpace when the volume holding path has
// less than minBytes free.  Unknown free space (unsupported platform)
// passes.
func checkFreeDisk(path string, minBytes uint64) error {
	free, ok := diskFreeBytes(path)
	if !ok {
		return nil
	}

	if free < minBytes {
		return fmt.Errorf("%w: %d MB free, need %d MB",
			ErrDiskSpace, free>>20, minBytes>>20)
	}

	return nil
}

// dumpSeriesRe extracts the monotonic series number NNNN from a dump
// URL or directory name (e.g. "listenbrainz-spark-dump-2593-…").
var dumpSeriesRe = regexp.MustCompile(`listenbrainz-(?:spark-)?dump-(\d+)-`)

// parseDumpSeries pulls the series number out of a dump URL/name.
func parseDumpSeries(url string) (int, bool) {
	m := dumpSeriesRe.FindStringSubmatch(url)
	if m == nil {
		return 0, false
	}

	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}

	return n, true
}

// Meta keys describing the catalog's provenance.  They are read by the
// client (the incremental refresh and the artifact import) and written
// by whichever path populated the index.
const (
	// dumpImportDoneKey marks a populated catalog in explore_index_meta.
	dumpImportDoneKey = "dump_import_done"

	// listensAppliedSeriesKey stores the listens dump series the
	// popularity numbers are folded up to — the high-water-mark the
	// incremental refresh resumes from.
	listensAppliedSeriesKey = "listens_applied_series"
)

// Entity kinds, used as the first byte of an mbidKey so one map can hold
// counts for all three entity types.
const (
	countKindRecording = byte(1)
	countKindRelease   = byte(2)
	countKindArtist    = byte(3)
)

// maxParquetMemberSize caps how large a single parquet member may be
// before it is treated as a malformed dump rather than buffered whole.
const maxParquetMemberSize = 1 << 30

// ErrDiskSpace is returned when free disk falls below the safety floor.
var ErrDiskSpace = errors.New("insufficient free disk space")

// parseUUID parses a canonical 36-char UUID string into 16 bytes.
// Returns false for anything malformed.
func parseUUID(s string, out []byte) bool {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}

	j := 0

	for i := 0; i < 36; i++ {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}

		hi := hexNibble(s[i])
		i++

		lo := hexNibble(s[i])
		if hi == 0xFF || lo == 0xFF {
			return false
		}

		out[j] = hi<<4 | lo
		j++
	}

	return true
}

func hexNibble(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	default:
		return 0xFF
	}
}

// ErrDumpFormat is returned when dump contents don't match the
// expected format.
var ErrDumpFormat = errors.New("unexpected dump format")
