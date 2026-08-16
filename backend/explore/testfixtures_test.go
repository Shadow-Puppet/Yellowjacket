package explore

import (
	"archive/tar"
	"bytes"
	"log/slog"
	"testing"

	"github.com/parquet-go/parquet-go"
)

// Fixtures shared by the client tests and the tagged index-builder
// tests.  They live in an untagged file so both builds compile.

// Fixed MBIDs for fixtures.
const (
	recA = "11111111-1111-1111-1111-111111111111"
	recB = "22222222-2222-2222-2222-222222222222"
	recC = "33333333-3333-3333-3333-333333333333"

	// recD is on relA and nobody has ever played it, which is the point:
	// it must count toward relA's track total without being indexed as a
	// recording itself.
	recD = "44444444-4444-4444-4444-444444444444"
	relA = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	relB = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	rgA  = "cccccccc-cccc-cccc-cccc-cccccccccccc"
	rgB  = "dddddddd-dddd-dddd-dddd-dddddddddddd"
	artA = "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"
	artB = "ffffffff-ffff-ffff-ffff-ffffffffffff"
)

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func makeTar(t *testing.T, members map[string][]byte, order []string) []byte {
	t.Helper()

	var buf bytes.Buffer

	tw := tar.NewWriter(&buf)

	for _, name := range order {
		data := members[name]
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(data)),
			Typeflag: tar.TypeReg,
		}

		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar header: %v", err)
		}

		if _, err := tw.Write(data); err != nil {
			t.Fatalf("tar write: %v", err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}

	return buf.Bytes()
}

func makeParquet(t *testing.T, rows []sparkFixtureRow) []byte {
	t.Helper()

	var buf bytes.Buffer

	w := parquet.NewGenericWriter[sparkFixtureRow](&buf)

	if _, err := w.Write(rows); err != nil {
		t.Fatalf("parquet write: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("parquet close: %v", err)
	}

	return buf.Bytes()
}

// sparkFixtureRow mimics the real spark listens schema: the aggregator
// must project just recording/release/artist MBIDs out of it.
type sparkFixtureRow struct {
	ListenedAt    int64    `parquet:"listened_at"`
	UserID        int64    `parquet:"user_id"`
	ArtistName    string   `parquet:"artist_name,optional"`
	RecordingMBID string   `parquet:"recording_mbid,optional"`
	ReleaseMBID   string   `parquet:"release_mbid,optional"`
	ArtistMBIDs   []string `parquet:"artist_credit_mbids,optional,list"`
}

// listensOf builds n identical listen rows for a recording.
func listensOf(n int, recording, release string, artists []string) []sparkFixtureRow {
	rows := make([]sparkFixtureRow, n)
	for i := range rows {
		rows[i] = sparkFixtureRow{
			ListenedAt:    1700000000 + int64(i),
			UserID:        int64(i),
			ArtistName:    "Fixture Artist",
			RecordingMBID: recording,
			ReleaseMBID:   release,
			ArtistMBIDs:   artists,
		}
	}

	return rows
}
