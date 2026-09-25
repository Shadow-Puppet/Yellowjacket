package explore

import (
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yellowjacket/backend/database"
)

// Import of the artifact we actually publish, as a client imports it.
//
// Every other test here builds a fixture, and a fixture is a second
// description of the storage format that can be wrong in the same
// direction as the code reading it.  That is how #258 shipped: the
// importer positioned its batch walk with a Go `string` cursor against
// the artifact's 16-byte `mbid` column, and SQLite neither coerces
// between TEXT and BLOB nor complains about the comparison — so the walk
// merged nothing and never advanced, and no install could finish its
// first index build.  The fixture that guards the walk writes the old
// text encoding; the only compact fixture is one row, below the batch
// size, so the bound query never ran.  Both passed throughout.
//
// So this one takes the published file and runs the client's own path
// over it — checksum, decompress, merge — and asserts that what the
// artifact holds is what the client ends up with.
//
// It skips without the path, so an ordinary test run pays nothing for
// it, and the publish job is where it is meant to run:
//
//	YJ_CORE_INDEX_ARTIFACT=/tmp/core-index.db.zst \
//	  go test -tags indexbuild -run TestImportPublishedArtifact \
//	  ./backend/explore/
//
// The indexbuild tag is not incidental: that job's container has no GTK,
// and the default tag set links the app through Wails.

// publishedArtifactEnv points at the published artifact: the compressed
// core-index.db.zst, or the unpacked core-index.db.
const publishedArtifactEnv = "YJ_CORE_INDEX_ARTIFACT"

// artifactTotals is the pair this test compares across the boundary.
//
// Rows is the whole point — a merge that lands fewer of them than the
// artifact declares is a catalog that looks populated and is missing
// things nobody can name — and popularity is the half whose absence was
// reported when it happened, because it arrives only through the merge.
type artifactTotals struct {
	rows       int
	withListen int
}

func TestImportPublishedArtifact(t *testing.T) {
	published := strings.TrimSpace(os.Getenv(publishedArtifactEnv))
	if published == "" {
		t.Skipf("set %s=<core-index.db.zst> to import the published artifact",
			publishedArtifactEnv)
	}

	if _, err := os.Stat(published); err != nil {
		t.Fatalf("%s: %v", publishedArtifactEnv, err)
	}

	// A file-backed database rather than NewTestDB's in-memory one: the
	// artifact is ~135MB and a million rows, which is not a thing to hold
	// in RAM inside a test.  YJ_HOME is how NewDB is pointed somewhere
	// disposable, and going through NewDB means this is the constructor,
	// the schema and the read pool the app itself opens.
	//
	// Nothing closes it, because nothing can: `DB` has no Close and the
	// app's handles are process-lifetime by design.  The directory is
	// unlinked at cleanup and the file goes with it.
	t.Setenv("YJ_HOME", t.TempDir())

	db, err := database.NewDB(testLogger())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}

	si := NewSearchIndex(db, nil, nil, testLogger())

	// The checksum the publisher shipped, if it shipped one.  Every
	// client verifies it and refuses the artifact when it does not
	// match, so a wrong one breaks Explore for everyone who has not
	// already imported — and nothing else would see it, because the
	// comparison is between two files only the publisher has.
	if want, ok := publishedChecksum(published); ok {
		got, err := fileSHA256(published)
		if err != nil {
			t.Fatalf("checksum the artifact: %v", err)
		}

		if got != want {
			t.Errorf("published artifact hashes to %s, but its .sha256 says %s",
				got, want)
		}
	}

	unpacked := unpackPublishedArtifact(t, si, published)

	want, err := artifactTotalsOf(unpacked)
	if err != nil {
		t.Fatalf("count the artifact's rows: %v", err)
	}

	if err := si.importCoreArtifact(context.Background(), unpacked); err != nil {
		t.Fatalf("importCoreArtifact: %v", err)
	}

	got, err := indexTotalsOf(db)
	if err != nil {
		t.Fatalf("count the index's rows: %v", err)
	}

	if got.rows != want.rows {
		t.Errorf("merged %d rows, but the artifact holds %d",
			got.rows, want.rows)
	}

	if got.withListen != want.withListen {
		t.Errorf("%d rows carry a listen count, but the artifact holds %d of them",
			got.withListen, want.withListen)
	}

	// The FTS index is rebuilt from the table once the merge is done, and
	// it is what search actually reads: a merge that lands without it
	// leaves Explore silently matching nothing, which is the state #258
	// produced by a different route.
	var indexed int
	if err := db.QueryRowWriter(
		"SELECT COUNT(*) FROM explore_index_fts",
	).Scan(&indexed); err != nil {
		t.Fatalf("count the FTS index: %v", err)
	}

	if indexed != got.rows {
		t.Errorf("FTS index holds %d rows against the table's %d",
			indexed, got.rows)
	}

	// And one row read back through the app's own path, which is the
	// other direction of every conversion the merge makes: a byte MBID
	// out of the table, the app's dashed form, and back in as a lookup.
	var raw []byte
	if err := db.QueryRowWriter(`
		SELECT mbid FROM explore_index
		WHERE entity_type = 1 /* artist */ AND popularity > 0
		ORDER BY popularity DESC LIMIT 1`).Scan(&raw); err != nil {
		t.Fatalf("read a stored mbid: %v", err)
	}

	dashed, err := mbidFromBytes(raw)
	if err != nil {
		t.Fatalf("the stored mbid is not one: %v", err)
	}

	artist := si.LookupArtistByMBID(dashed)
	if artist == nil {
		t.Fatalf("the artifact's most popular artist %s does not look up", dashed)
	}

	if artist.Popularity == 0 {
		t.Errorf("artist %s came back with no popularity", dashed)
	}
}

// publishedChecksum reads the sha256 the publisher wrote beside the
// artifact, in `sha256sum` output form.  A missing file is not a
// failure: it is only there when the artifact came from the publish job.
func publishedChecksum(path string) (string, bool) {
	body, err := os.ReadFile(path + ".sha256")
	if err != nil {
		return "", false
	}

	sum := strings.TrimSpace(string(body))
	if i := strings.IndexAny(sum, " \t"); i > 0 {
		sum = sum[:i]
	}

	if len(sum) != 64 {
		return "", false
	}

	return strings.ToLower(sum), true
}

// unpackPublishedArtifact returns a path to the unpacked database,
// going through the client's own decompression when it is handed the
// compressed file that is actually published.
func unpackPublishedArtifact(t *testing.T, si *SearchIndex, path string) string {
	t.Helper()

	if strings.HasSuffix(path, ".db") {
		return path
	}

	// Copied into the test's own directory first: decompress writes
	// beside the compressed file, and the publisher's directory is not
	// this test's to write in.
	staging := t.TempDir()
	dst := filepath.Join(staging, coreArtifactFile)

	src, err := os.Open(path)
	if err != nil {
		t.Fatalf("open the published artifact: %v", err)
	}

	defer func() { _ = src.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("create a staging copy: %v", err)
	}

	if _, err := io.Copy(out, src); err != nil {
		t.Fatalf("copy the published artifact: %v", err)
	}

	if err := out.Close(); err != nil {
		t.Fatalf("close the staging copy: %v", err)
	}

	fetcher := &artifactFetcher{si: si, stagingDir: staging}

	if err := fetcher.decompress(context.Background()); err != nil {
		t.Fatalf("decompress the published artifact: %v", err)
	}

	return fetcher.unpackedPath()
}

// artifactTotalsOf counts what an artifact file holds, read directly so
// the numbers do not depend on anything the client does.
func artifactTotalsOf(path string) (artifactTotals, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return artifactTotals{}, err
	}

	defer func() { _ = db.Close() }()

	var totals artifactTotals

	err = db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(popularity > 0), 0)
		FROM explore_index`).Scan(&totals.rows, &totals.withListen)
	if err != nil {
		return artifactTotals{}, err
	}

	return totals, nil
}

// indexTotalsOf counts what the client ended up with.
func indexTotalsOf(db *database.DB) (artifactTotals, error) {
	var totals artifactTotals

	err := db.QueryRowWriter(`SELECT COUNT(*), COALESCE(SUM(popularity > 0), 0)
		FROM explore_index`).Scan(&totals.rows, &totals.withListen)

	return totals, err
}
