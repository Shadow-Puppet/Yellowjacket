//go:build indexbuild

package explore

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"yellowjacket/backend/database"
)

// tarOf builds an uncompressed tar of the named members, in the order
// given.  Order is the point of several of these tests: the real dump's
// members are alphabetical, which is what lets one pass resolve an
// entity's credit without buffering 35M recordings.
func tarOf(t *testing.T, members ...[2]string) *tar.Reader {
	t.Helper()

	var buf bytes.Buffer

	tw := tar.NewWriter(&buf)

	for _, m := range members {
		body := []byte(m[1])

		if err := tw.WriteHeader(&tar.Header{
			Name:     "mbdump/" + m[0],
			Mode:     0o644,
			Size:     int64(len(body)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatalf("tar header: %v", err)
		}

		if _, err := tw.Write(body); err != nil {
			t.Fatalf("tar write: %v", err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}

	return tar.NewReader(&buf)
}

func tsv(rows ...[]string) string {
	var b strings.Builder

	for _, r := range rows {
		b.WriteString(strings.Join(r, "\t"))
		b.WriteByte('\n')
	}

	return b.String()
}

// mustMBID is testMBID in the packed form the catalog stores.
func mustMBID(label string) uuid16 {
	var u uuid16

	if !parseUUID(testMBID(label), u[:]) {
		panic("testMBID did not produce a UUID for " + label)
	}

	return u
}

// The two artists of the worked example, and the entities they credit.
var (
	creditRecMBID = mustMBID("recording-1")
	creditRGMBID  = mustMBID("release-group-1")
)

// sampleDump is the shape verified against the 20260815 export:
// artist(id, gid, ...), artist_credit(id, name, artist_count, ...),
// artist_credit_name(credit, position, artist, name, join_phrase),
// recording/release_group(id, gid, name, artist_credit, ...).
func sampleDump(t *testing.T) *tar.Reader {
	t.Helper()

	return tarOf(t,
		[2]string{"artist", tsv(
			[]string{"11", testMBID("artist-a"), "Snoop Doggy Dogg", "Snoop Doggy Dogg"},
			[]string{"22", testMBID("artist-b"), "2Pac", "2Pac"},
		)},
		[2]string{"artist_credit", tsv(
			[]string{"900", "2Pac feat. Snoop Dogg", "2", "1", "", "0", ""},
			[]string{"901", "Solo Artist", "1", "1", "", "0", ""},
		)},
		[2]string{"artist_credit_name", tsv(
			// Deliberately out of position order: the dump is not
			// obliged to emit them sorted and the credit's meaning is
			// the order, not the file's.
			[]string{"900", "1", "11", "Snoop Dogg", ""},
			[]string{"900", "0", "22", "2Pac", " feat. "},
			[]string{"901", "0", "11", "Solo Artist", ""},
		)},
		[2]string{"recording", tsv(
			[]string{"1", testMBID("recording-1"), "Some Song", "900", "180000"},
			[]string{"2", testMBID("not-kept"), "Other", "900", "1"},
			[]string{"3", testMBID("solo"), "Solo", "901", "1"},
		)},
		[2]string{"release_group", tsv(
			[]string{"5", testMBID("release-group-1"), "Some Album", "900", "1"},
		)},
	)
}

func creditTestImporter(t *testing.T) *dumpImporter {
	t.Helper()

	db := database.NewTestDB(t)

	return &dumpImporter{
		si:     NewSearchIndex(db, nil, nil, testLogger()),
		logger: testLogger(),
	}
}

// TestScanCreditDumpDecomposes is the worked example end to end: the
// credit's parts come back in position order, with the *credited*
// names and the join phrase between them.
func TestScanCreditDumpDecomposes(t *testing.T) {
	imp := creditTestImporter(t)

	kept := map[uuid16]struct{}{
		creditRecMBID: {},
		creditRGMBID:  {},
	}

	scan, err := imp.scanCreditTar(context.Background(), sampleDump(t), kept)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if got := len(scan.refs); got != 2 {
		t.Fatalf("refs = %d, want 2 (the recording and the release group)", got)
	}

	if scan.refs[creditRecMBID] != 900 {
		t.Errorf("recording credit = %d, want 900", scan.refs[creditRecMBID])
	}

	parts := scan.parts[900]
	if len(parts) != 2 {
		t.Fatalf("parts = %d, want 2", len(parts))
	}

	// Sorting happens on write, so assert the pieces are all present
	// and let the render test below check the order.
	byPos := map[int]creditPart{}
	for _, p := range parts {
		byPos[p.position] = p
	}

	if byPos[0].name != "2Pac" || byPos[0].join != " feat. " {
		t.Errorf("position 0 = %q/%q, want \"2Pac\"/\" feat. \"",
			byPos[0].name, byPos[0].join)
	}

	// The credited name, not the artist's own name: this is the whole
	// reason credited_name is stored per row.
	if byPos[1].name != "Snoop Dogg" {
		t.Errorf("position 1 credited name = %q, want \"Snoop Dogg\"", byPos[1].name)
	}
}

// TestSingleArtistCreditsAreNotStored: a one-artist credit is already
// described by explore_index's artist_name/artist_mbid, and storing it
// would roughly triple the table to say nothing new.
func TestSingleArtistCreditsAreNotStored(t *testing.T) {
	imp := creditTestImporter(t)

	solo := mustMBID("solo")
	kept := map[uuid16]struct{}{solo: {}}

	scan, err := imp.scanCreditTar(context.Background(), sampleDump(t), kept)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if len(scan.refs) != 0 {
		t.Fatalf("a single-artist credit was referenced: %v", scan.refs)
	}

	if _, ok := scan.multiCredits[901]; ok {
		t.Error("credit 901 has artist_count 1 and should not be multi")
	}
}

// TestOnlyKeptEntitiesAreReferenced: the catalog's popularity filter
// decides what is worth carrying credits for, and an entity outside it
// must not produce a row pointing at nothing.
func TestOnlyKeptEntitiesAreReferenced(t *testing.T) {
	imp := creditTestImporter(t)

	kept := map[uuid16]struct{}{creditRecMBID: {}}

	scan, err := imp.scanCreditTar(context.Background(), sampleDump(t), kept)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if _, ok := scan.refs[mustMBID("not-kept")]; ok {
		t.Error("an entity outside the catalog was referenced")
	}

	if len(scan.used) != 1 {
		t.Errorf("used credits = %d, want 1", len(scan.used))
	}
}

// TestWriteCreditsRoundTrips checks what the frontend will actually
// read: parts in position order, dashed MBIDs out of the 16 raw bytes,
// and a rendered credit that reassembles to the tagged string.
func TestWriteCreditsRoundTrips(t *testing.T) {
	imp := creditTestImporter(t)

	kept := map[uuid16]struct{}{creditRecMBID: {}, creditRGMBID: {}}

	scan, err := imp.scanCreditTar(context.Background(), sampleDump(t), kept)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if err := imp.writeCredits(context.Background(), scan); err != nil {
		t.Fatalf("writeCredits: %v", err)
	}

	rows, err := imp.si.db.QueryContext(
		`SELECT p.position, p.artist_mbid, p.credited_name, p.join_phrase
		 FROM artist_credit_ref r
		 JOIN artist_credit_part p ON p.credit_id = r.credit_id
		 WHERE r.mbid = ?
		 ORDER BY p.position`,
		creditRecMBID[:],
	)
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	defer func() { _ = rows.Close() }()

	var rendered strings.Builder

	names := []string{}

	for rows.Next() {
		var (
			pos  int
			mbid []byte
			name string
			join string
		)

		if err := rows.Scan(&pos, &mbid, &name, &join); err != nil {
			t.Fatalf("scan row: %v", err)
		}

		if len(mbid) != 16 {
			t.Fatalf("artist_mbid is %d bytes, want 16", len(mbid))
		}

		names = append(names, name)

		rendered.WriteString(name)
		rendered.WriteString(join)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	// Concatenation is the contract: names in order, join phrases
	// between them, and no searching a name inside a credit string.
	if got := rendered.String(); got != "2Pac feat. Snoop Dogg" {
		t.Errorf("rendered credit = %q, want %q", got, "2Pac feat. Snoop Dogg")
	}

	if len(names) != 2 || names[0] != "2Pac" {
		t.Errorf("parts came back out of position order: %v", names)
	}
}

// TestCreditRefsNeverDangle: a ref whose parts were not stored renders
// as a credit with no artists at all, which is worse than the
// single-artist fallback it replaced.
func TestCreditRefsNeverDangle(t *testing.T) {
	imp := creditTestImporter(t)

	kept := map[uuid16]struct{}{creditRecMBID: {}}

	scan, err := imp.scanCreditTar(context.Background(), sampleDump(t), kept)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	// An artist the dump never named: the credit cannot be navigated to
	// and must be dropped whole, taking its ref with it.
	scan.artistGIDs = map[int32]uuid16{}

	if err := imp.writeCredits(context.Background(), scan); err != nil {
		t.Fatalf("writeCredits: %v", err)
	}

	var refs, parts int

	if err := imp.si.db.QueryRowWriter(
		"SELECT COUNT(*) FROM artist_credit_ref",
	).Scan(&refs); err != nil {
		t.Fatalf("count refs: %v", err)
	}

	if err := imp.si.db.QueryRowWriter(
		"SELECT COUNT(*) FROM artist_credit_part",
	).Scan(&parts); err != nil {
		t.Fatalf("count parts: %v", err)
	}

	if refs != 0 || parts != 0 {
		t.Fatalf("refs=%d parts=%d, want 0/0 when the artists are unknown", refs, parts)
	}
}

// TestCreditDumpShapeIsAsserted: the dump has no header row, so a
// column that moved would be read as its neighbour and produce a
// catalog that is quietly wrong.  Loud is the requirement.
func TestCreditDumpShapeIsAsserted(t *testing.T) {
	imp := creditTestImporter(t)

	short := tarOf(t, [2]string{"artist", tsv([]string{"11", "only-two-columns"})})

	_, err := imp.scanCreditTar(context.Background(), short, map[uuid16]struct{}{})
	if err == nil {
		t.Fatal("a member with a non-UUID gid was accepted")
	}

	if !errors.Is(err, ErrDumpShape) {
		t.Errorf("error = %v, want ErrDumpShape", err)
	}
}

// TestUnescapeCopy covers Postgres COPY's text escaping, which reaches
// artist names routinely -- a tab or backslash in a name would
// otherwise shift every field after it.
func TestUnescapeCopy(t *testing.T) {
	tests := []struct{ in, want string }{
		{`plain`, `plain`},
		{`\N`, ``},
		{`a\tb`, "a\tb"},
		{`a\nb`, "a\nb"},
		{`back\\slash`, `back\slash`},
		{`AC\/DC`, `AC\/DC`},
		{`trailing\`, `trailing\`},
	}

	for _, tt := range tests {
		if got := unescapeCopy(tt.in); got != tt.want {
			t.Errorf("unescapeCopy(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestEnsureArtistCreditsIsIdempotent pins what the index job depends
// on to decide whether to publish.
//
// The pass runs on every mode, including the `refresh` that a complete
// catalog always chooses — so it must be free when there is nothing to
// do, and it must say so.  A `true` here republishes the artifact; a
// `true` on every run would republish an identical one weekly, and a
// permanent `false` would mean a catalog that never gains credits at
// all.
func TestEnsureArtistCreditsIsIdempotent(t *testing.T) {
	imp := creditTestImporter(t)

	// The marker is what "already done" means; with it set, the pass
	// must not reach the network or report a change.
	imp.si.setMeta(creditsImportDoneKey, "1")

	if imp.ensureArtistCredits(context.Background()) {
		t.Fatal("a second run reported new credits; the artifact would republish forever")
	}
}

// TestEnsureArtistCreditsReportsFailureAsNoChange: a dump that cannot be
// reached leaves the catalog exactly as it was, and must not claim
// otherwise — publishing on it would ship an artifact with no credits
// and mark the work done.
func TestEnsureArtistCreditsReportsFailureAsNoChange(t *testing.T) {
	imp := creditTestImporter(t)
	imp.httpClient = newDumpHTTPClient()
	imp.mbdumpBaseURL = "http://127.0.0.1:1/nonexistent/"

	if imp.ensureArtistCredits(context.Background()) {
		t.Fatal("an unreachable dump reported new credits")
	}

	if imp.si.hasMeta(creditsImportDoneKey) {
		t.Error("a failed pass marked itself done; it would never retry")
	}
}
