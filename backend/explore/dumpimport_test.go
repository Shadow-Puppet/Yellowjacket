//go:build indexbuild

package explore

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"yellowjacket/backend/database"
)

// ---------------------------------------------------------------------------
// Unit tests: parsing helpers
// ---------------------------------------------------------------------------

func TestParseUUIDRoundTrip(t *testing.T) {
	var buf [16]byte

	if !parseUUID(recA, buf[:]) {
		t.Fatalf("parseUUID rejected valid UUID %s", recA)
	}

	if got := formatUUID(buf[:]); got != recA {
		t.Fatalf("round trip = %q, want %q", got, recA)
	}

	invalid := []string{
		"", "not-a-uuid",
		"11111111-1111-1111-1111-11111111111",   // too short
		"11111111-1111-1111-1111-1111111111111", // too long
		"1111111101111-1111-1111-111111111111",  // bad dash
		"gggggggg-1111-1111-1111-111111111111",  // bad hex
	}

	for _, s := range invalid {
		if parseUUID(s, buf[:]) {
			t.Errorf("parseUUID accepted invalid input %q", s)
		}
	}
}

func TestParsePGStringArray(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"{" + artA + "}", []string{artA}},
		{"{" + artA + "," + artB + "}", []string{artA, artB}},
		{`{"` + artA + `","` + artB + `"}`, []string{artA, artB}},
		{"['" + artA + "', '" + artB + "']", []string{artA, artB}},
		{artA, []string{artA}},
		{"", nil},
		{"{}", nil},
	}

	for _, c := range cases {
		got := parsePGStringArray(c.in)
		if len(got) != len(c.want) {
			t.Errorf("parsePGStringArray(%q) = %v, want %v", c.in, got, c.want)

			continue
		}

		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("parsePGStringArray(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestFloorForBudget(t *testing.T) {
	vals := []uint32{100, 90, 80, 70, 60, 50, 40, 30, 20, 5}

	if got := floorForBudget(vals, 3, 10); got != 80 {
		t.Errorf("budget 3: floor = %d, want 80", got)
	}

	// Budget larger than data → min floor.
	if got := floorForBudget(vals, 100, 10); got != 10 {
		t.Errorf("budget 100: floor = %d, want 10", got)
	}

	// Floor clamped up to minFloor.
	if got := floorForBudget(vals, 10, 10); got != 10 {
		t.Errorf("clamp: floor = %d, want 10", got)
	}

	if got := floorForBudget(nil, 5, 7); got != 7 {
		t.Errorf("empty: floor = %d, want 7", got)
	}
}

func TestRankFloor(t *testing.T) {
	desc := []uint32{100, 90, 80, 70, 60}

	cases := []struct {
		rank int
		want uint32
	}{
		{1, 100},
		{3, 80},
		{5, 60},
		{99, 60}, // clamped to the last element
	}

	for _, c := range cases {
		if got := rankFloor(desc, c.rank); got != c.want {
			t.Errorf("rankFloor(rank=%d) = %d, want %d", c.rank, got, c.want)
		}
	}

	if got := rankFloor(nil, 3); got != 0 {
		t.Errorf("rankFloor(empty) = %d, want 0", got)
	}
}

func TestTierBudget(t *testing.T) {
	const aFloor, bFloor = uint32(1000), uint32(100)

	cases := []struct {
		listens           uint32
		wantTrack, wantRG int
	}{
		{2000, perArtistTierATrack, perArtistTierARG}, // tier A
		{1000, perArtistTierATrack, perArtistTierARG}, // exactly on A floor
		{500, perArtistTierBTrack, perArtistTierBRG},  // tier B
		{100, perArtistTierBTrack, perArtistTierBRG},  // exactly on B floor
		{10, perArtistTierCTrack, perArtistTierCRG},   // tier C
	}

	for _, c := range cases {
		gotTrack, gotRG := tierBudget(c.listens, aFloor, bFloor)
		if gotTrack != c.wantTrack || gotRG != c.wantRG {
			t.Errorf("tierBudget(%d) = (%d, %d), want (%d, %d)",
				c.listens, gotTrack, gotRG, c.wantTrack, c.wantRG)
		}
	}
}

// mkMBID builds a distinct uuid16 from a single byte, for heap tests.
func mkMBID(b byte) uuid16 {
	var id uuid16

	id[0] = b

	return id
}

func TestArtistTopNBoundedAndDeduped(t *testing.T) {
	a := &artistTopN{n: 3, inSet: make(map[uuid16]struct{})}

	// Add five distinct recordings; only the top 3 by listens survive.
	for i, listens := range []uint32{10, 50, 30, 5, 40} {
		a.add(keptRecordingRow{mbid: mkMBID(byte(i + 1)), listens: listens})
	}

	if len(a.rows) != 3 {
		t.Fatalf("len = %d, want 3 (bounded)", len(a.rows))
	}

	got := map[uint32]bool{}
	for _, r := range a.rows {
		got[r.listens] = true
	}

	for _, want := range []uint32{50, 40, 30} {
		if !got[want] {
			t.Errorf("expected top listens %d retained, have %v", want, got)
		}
	}

	if got[10] || got[5] {
		t.Errorf("evicted entries survived: %v", got)
	}

	// Re-adding an existing MBID is a no-op, even with a higher count.
	before := len(a.rows)

	a.add(keptRecordingRow{mbid: mkMBID(2), listens: 9999})

	if len(a.rows) != before {
		t.Errorf("duplicate MBID grew the set: %d != %d", len(a.rows), before)
	}

	if _, dupHigh := got[9999]; dupHigh {
		t.Error("duplicate MBID should not have been re-ranked")
	}
}

func TestArtistTopRGBoundedAndDeduped(t *testing.T) {
	a := &artistTopRG{n: 2, inSet: make(map[uuid16]struct{})}

	a.add(mkMBID(1), 100)
	a.add(mkMBID(2), 200)
	a.add(mkMBID(3), 50)  // below both, dropped
	a.add(mkMBID(1), 999) // duplicate, ignored

	if len(a.rgs) != 2 {
		t.Fatalf("len = %d, want 2", len(a.rgs))
	}

	for _, c := range a.rgs {
		if c.listens == 50 {
			t.Error("sub-threshold RG was kept")
		}

		if c.rg == mkMBID(1) && c.listens != 100 {
			t.Errorf("duplicate RG re-ranked to %d", c.listens)
		}
	}
}

// ---------------------------------------------------------------------------
// Fixture builders
// ---------------------------------------------------------------------------

func zstdCompress(t *testing.T, data []byte) []byte {
	t.Helper()

	var buf bytes.Buffer

	zw, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatalf("zstd writer: %v", err)
	}

	if _, err := zw.Write(data); err != nil {
		t.Fatalf("zstd write: %v", err)
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("zstd close: %v", err)
	}

	return buf.Bytes()
}

func csvBytes(t *testing.T, rows [][]string) []byte {
	t.Helper()

	var buf bytes.Buffer

	w := csv.NewWriter(&buf)
	if err := w.WriteAll(rows); err != nil {
		t.Fatalf("csv write: %v", err)
	}

	return buf.Bytes()
}

// canonicalDataCSV builds a canonical_musicbrainz_data.csv fixture.
func canonicalDataCSV(t *testing.T) []byte {
	t.Helper()

	rows := [][]string{
		{
			"id", "artist_credit_id", "artist_mbids", "artist_credit_name",
			"release_mbid", "release_name", "recording_mbid", "recording_name",
			"combined_lookup", "score",
		},
		{"1", "10", "{" + artA + "}", "Solo Star", relA, "Big Album", recA, "Hit Song", "x", "1"},
		{"2", "10", "{" + artA + "}", "Solo Star", relA, "Big Album", recB, "Deep Cut", "x", "1"},
		{
			"3", "11", "{" + artA + "," + artB + "}", "Solo Star feat. Guest",
			relB, "Duet Album", recC, "Duet Song", "x", "1",
		},
		// Unplayed, so it survives no popularity floor and is not
		// indexed -- but it is still a track on Big Album.
		{
			"4", "10", "{" + artA + "}", "Solo Star",
			relA, "Big Album", recD, "Album Filler", "x", "1",
		},
	}

	return csvBytes(t, rows)
}

// canonicalRedirectCSV builds a canonical_release_redirect.csv fixture.
func canonicalRedirectCSV(t *testing.T) []byte {
	t.Helper()

	rows := [][]string{
		{"release_mbid", "canonical_release_mbid", "release_group_mbid"},
		{relA, relA, rgA},
		{relB, relB, rgB},
	}

	return csvBytes(t, rows)
}

// serveDumps returns an httptest server presenting MetaBrainz-style
// listing pages and Range-capable dump files.
func serveDumps(t *testing.T, sparkTar, canonicalTarZst []byte) *httptest.Server {
	t.Helper()

	const (
		listensDir   = "listenbrainz-dump-1-20260101-000003-full"
		sparkFile    = "listenbrainz-spark-dump-1-20260101-000003-full.tar"
		canonicalDir = "musicbrainz-canonical-dump-20260101-080003"
		canonicalTar = "musicbrainz-canonical-dump-20260101-080003.tar.zst"
	)

	modTime := time.Now()
	mux := http.NewServeMux()

	mux.HandleFunc("/listens/", func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/listens/") {
		case "":
			_, _ = fmt.Fprintf(w, `<a href="%s/">%s/</a>`, listensDir, listensDir)
		case listensDir + "/":
			_, _ = fmt.Fprintf(w, `<a href="%s">%s</a>`, sparkFile, sparkFile)
		case listensDir + "/" + sparkFile:
			http.ServeContent(w, r, sparkFile, modTime, bytes.NewReader(sparkTar))
		default:
			http.NotFound(w, r)
		}
	})

	mux.HandleFunc("/canonical/", func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/canonical/") {
		case "":
			_, _ = fmt.Fprintf(w, `<a href="%s/">%s/</a>`, canonicalDir, canonicalDir)
		case canonicalDir + "/":
			_, _ = fmt.Fprintf(w, `<a href="%s">%s</a>`, canonicalTar, canonicalTar)
		case canonicalDir + "/" + canonicalTar:
			http.ServeContent(w, r, canonicalTar, modTime, bytes.NewReader(canonicalTarZst))
		default:
			http.NotFound(w, r)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

func testImporter(t *testing.T, si *SearchIndex, srv *httptest.Server) *dumpImporter {
	t.Helper()

	return &dumpImporter{
		si:               si,
		lb:               nil, // patch passes skipped in tests
		logger:           testLogger(),
		httpClient:       srv.Client(),
		stagingDir:       t.TempDir(),
		canonicalBaseURL: srv.URL + "/canonical/",
		listensBaseURL:   srv.URL + "/listens/",
	}
}

func fixtureSparkTar(t *testing.T) []byte {
	t.Helper()

	// Member 1: recA is popular (12 listens).  Member 2: recB has 11,
	// recC has 12 (multi-artist credit).  Totals: artA = 35, artB = 12.
	member1 := makeParquet(t, listensOf(12, recA, relA, []string{artA}))
	member2 := makeParquet(t, append(
		listensOf(11, recB, relA, []string{artA}),
		listensOf(12, recC, relB, []string{artA, artB})...,
	))

	prefix := "listenbrainz-spark-dump-1-20260101-000003-full/listens/"

	return makeTar(t,
		map[string][]byte{
			prefix + "1.parquet": member1,
			prefix + "2.parquet": member2,
		},
		[]string{prefix + "1.parquet", prefix + "2.parquet"},
	)
}

func fixtureCanonicalTarZst(t *testing.T) []byte {
	t.Helper()

	prefix := "musicbrainz-canonical-dump-20260101-080003/"

	raw := makeTar(t,
		map[string][]byte{
			prefix + "canonical_musicbrainz_data.csv":   canonicalDataCSV(t),
			prefix + "canonical_release_redirect.csv":   canonicalRedirectCSV(t),
			prefix + "canonical_recording_redirect.csv": {},
		},
		[]string{
			prefix + "canonical_release_redirect.csv",
			prefix + "canonical_musicbrainz_data.csv",
			prefix + "canonical_recording_redirect.csv",
		},
	)

	return zstdCompress(t, raw)
}

// ---------------------------------------------------------------------------
// Stage tests
// ---------------------------------------------------------------------------

func TestAggregateListenCounts(t *testing.T) {
	srv := serveDumps(t, fixtureSparkTar(t), nil)

	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, testLogger())
	imp := testImporter(t, si, srv)

	sparkURL, err := discoverDumpFile(
		context.Background(), imp.httpClient, imp.listensBaseURL, listensDirRe, sparkFileRe,
	)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}

	st := &countsState{SparkURL: sparkURL}
	if err := imp.aggregateListenCounts(context.Background(), st); err != nil {
		t.Fatalf("aggregate: %v", err)
	}

	assertCount := func(kind byte, mbid string, want uint32) {
		t.Helper()

		key, ok := makeMBIDKey(kind, mbid)
		if !ok {
			t.Fatalf("bad fixture mbid %s", mbid)
		}

		if got := st.counts[key]; got != want {
			t.Errorf("count(kind=%d, %s) = %d, want %d", kind, mbid, got, want)
		}
	}

	assertCount(countKindRecording, recA, 12)
	assertCount(countKindRecording, recB, 11)
	assertCount(countKindRecording, recC, 12)
	assertCount(countKindRelease, relA, 23)
	assertCount(countKindRelease, relB, 12)
	assertCount(countKindArtist, artA, 35)
	assertCount(countKindArtist, artB, 12)

	if !st.Done {
		t.Error("state not marked done")
	}

	// The checkpoint file round-trips.
	loaded, err := imp.readCountsFile()
	if err != nil {
		t.Fatalf("read counts file: %v", err)
	}

	if loaded == nil || !loaded.Done || len(loaded.counts) != len(st.counts) {
		t.Fatalf("checkpoint mismatch: %+v", loaded)
	}
}

func TestAggregateResumeFromOffset(t *testing.T) {
	sparkTar := fixtureSparkTar(t)
	srv := serveDumps(t, sparkTar, nil)

	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, testLogger())
	imp := testImporter(t, si, srv)

	sparkURL := srv.URL + "/listens/listenbrainz-dump-1-20260101-000003-full/listenbrainz-spark-dump-1-20260101-000003-full.tar"

	// Full run for reference.
	full := &countsState{SparkURL: sparkURL}
	if err := imp.aggregateListenCounts(context.Background(), full); err != nil {
		t.Fatalf("full aggregate: %v", err)
	}

	// Simulate a checkpoint taken after member 1: offset = header
	// block + padded member-1 size (fixture names are short, so the
	// header is a single 512-byte block).
	member1 := makeParquet(t, listensOf(12, recA, relA, []string{artA}))
	offset := int64(512) + (int64(len(member1))+511)/512*512

	key, _ := makeMBIDKey(countKindRecording, recA)
	relKey, _ := makeMBIDKey(countKindRelease, relA)
	artKey, _ := makeMBIDKey(countKindArtist, artA)

	resumed := &countsState{
		SparkURL:  sparkURL,
		Offset:    offset,
		MemberIdx: 1,
		counts: map[mbidKey]uint32{
			key:    12,
			relKey: 12,
			artKey: 12,
		},
	}

	if err := imp.aggregateListenCounts(context.Background(), resumed); err != nil {
		t.Fatalf("resumed aggregate: %v", err)
	}

	if len(resumed.counts) != len(full.counts) {
		t.Fatalf("resumed entities = %d, want %d", len(resumed.counts), len(full.counts))
	}

	for k, want := range full.counts {
		if got := resumed.counts[k]; got != want {
			t.Errorf("resumed count %s = %d, want %d (double count?)", formatUUID(k[1:]), got, want)
		}
	}
}

func TestResumableReaderReconnects(t *testing.T) {
	payload := bytes.Repeat([]byte("0123456789abcdef"), 4096) // 64KB

	// A flaky server that truncates every response to 10KB, forcing
	// the reader to reconnect with Range requests.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset := int64(0)
		if rng := r.Header.Get("Range"); rng != "" {
			_, _ = fmt.Sscanf(rng, "bytes=%d-", &offset)
		}

		chunk := payload[offset:min(offset+10240, int64(len(payload)))]

		w.Header().Set("Content-Range",
			fmt.Sprintf("bytes %d-%d/%d", offset, offset+int64(len(chunk))-1, len(payload)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(chunk)
	}))
	t.Cleanup(srv.Close)

	r := newResumableReader(context.Background(), srv.Client(), srv.URL, 0)

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch: got %d bytes, want %d", len(got), len(payload))
	}
}

// ---------------------------------------------------------------------------
// End-to-end
// ---------------------------------------------------------------------------

func TestDumpImportEndToEnd(t *testing.T) {
	srv := serveDumps(t, fixtureSparkTar(t), fixtureCanonicalTarZst(t))

	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, testLogger())
	imp := testImporter(t, si, srv)
	stagingDir := imp.stagingDir

	// A legacy API-crawled row with inflated popularity must be
	// cleared by the first dump import (scale consistency).
	if err := imp.run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	// The import bulk-loads with the FTS sync triggers suspended, so
	// the rebuild that closes that window is the only thing keeping
	// search usable: assembled rows must be findable afterwards.
	ftsCount := func(query string) int {
		t.Helper()

		rows, err := db.QueryContext(
			"SELECT COUNT(*) FROM explore_index_fts WHERE explore_index_fts MATCH ?", query,
		)
		if err != nil {
			t.Fatalf("fts query %q: %v", query, err)
		}

		defer func() { _ = rows.Close() }()

		n := 0

		if rows.Next() {
			_ = rows.Scan(&n)
		}

		return n
	}

	if got := ftsCount("Song"); got == 0 {
		t.Error("FTS matches no assembled recordings; the rebuild did not run")
	}

	// Index rows landed with dump-derived popularity.
	assertRow := func(mbid, entityType, title string, popularity int) {
		t.Helper()

		rows, err := db.QueryContext(
			"SELECT title, popularity FROM explore_index WHERE mbid = ? AND entity_type = ?",
			dbMBID(mbid), dbEntityType(entityType),
		)
		if err != nil {
			t.Fatalf("query: %v", err)
		}

		defer func() { _ = rows.Close() }()

		if !rows.Next() {
			t.Fatalf("no %s row for %s", entityType, mbid)
		}

		var gotTitle string

		var gotPop int

		if err := rows.Scan(&gotTitle, &gotPop); err != nil {
			t.Fatalf("scan: %v", err)
		}

		if gotTitle != title || gotPop != popularity {
			t.Errorf("%s %s = (%q, %d), want (%q, %d)",
				entityType, mbid, gotTitle, gotPop, title, popularity)
		}
	}

	assertRow(recA, "recording", "Hit Song", 12)
	assertRow(recB, "recording", "Deep Cut", 11)
	assertRow(recC, "recording", "Duet Song", 12)
	assertRow(rgA, "release_group", "Big Album", 23)
	assertRow(rgB, "release_group", "Duet Album", 12)
	assertRow(artA, "artist", "Solo Star", 35)

	// The per-release-group denominator: how many tracks the release
	// has, counted from the canonical dump *before* the popularity
	// filter.  Big Album has three, one of which nobody has played and
	// which is therefore not indexed as a recording at all -- a
	// denominator built from the kept recordings would say two, and
	// "you have 2 of 2" about a three-track album is worse than saying
	// nothing.
	assertTotalTracks := func(mbid string, want int) {
		t.Helper()

		var got int

		if err := db.QueryRowWriter(
			"SELECT total_tracks FROM explore_index WHERE mbid = ? AND entity_type = 2",
			dbMBID(mbid),
		).Scan(&got); err != nil {
			t.Fatalf("read total_tracks for %s: %v", mbid, err)
		}

		if got != want {
			t.Errorf("total_tracks for %s = %d, want %d", mbid, got, want)
		}
	}

	assertTotalTracks(rgA, 3)
	assertTotalTracks(rgB, 1)

	// And the unplayed track is still not an indexed recording.
	var fillerRows int

	if err := db.QueryRowWriter(
		"SELECT COUNT(*) FROM explore_index WHERE mbid = ?", dbMBID(recD),
	).Scan(&fillerRows); err != nil {
		t.Fatalf("count recD rows: %v", err)
	}

	if fillerRows != 0 {
		t.Errorf("unplayed track was indexed as a recording (%d rows)", fillerRows)
	}

	// artB only ever appears in a multi-artist credit: no name is
	// derivable from the dump, so it must be queued for the API
	// metadata patch instead of being written nameless.
	rows, err := db.QueryContext(
		"SELECT COUNT(*) FROM explore_index WHERE mbid = ?", dbMBID(artB),
	)
	if err != nil {
		t.Fatalf("query artB: %v", err)
	}

	if rows.Next() {
		var n int

		_ = rows.Scan(&n)

		if n != 0 {
			t.Errorf("artB row written without a name source")
		}
	}

	_ = rows.Close()

	found := false

	for _, mbid := range imp.pendingArtists {
		if mbid == artB {
			found = true
		}
	}

	if !found {
		t.Errorf("artB not queued for metadata patch: %v", imp.pendingArtists)
	}

	// FTS search works end to end.
	si.MarkReadyIfPopulated()

	results := si.Search(context.Background(), "hit song", 10)
	if len(results) == 0 || results[0].MBID != recA {
		t.Fatalf("search for indexed recording failed: %+v", results)
	}

	// Completion recorded; staging cleaned up.
	if !si.hasMeta(dumpImportDoneKey) {
		t.Error("dump_import_done not recorded")
	}

	// The incremental refresh baseline was recorded, and the
	// release→release-group map was persisted for future rollups.
	if _, ok := si.metaInt(listensAppliedSeriesKey); !ok {
		t.Error("listens_applied_series baseline not recorded")
	}

	var relToRGRows int

	rtrRows, err := db.QueryContext("SELECT COUNT(*) FROM release_to_rg")
	if err != nil {
		t.Fatalf("query release_to_rg: %v", err)
	}

	if rtrRows.Next() {
		_ = rtrRows.Scan(&relToRGRows)
	}

	_ = rtrRows.Close()

	if relToRGRows == 0 {
		t.Error("release_to_rg not populated after import")
	}

	if _, err := os.Stat(stagingDir); !os.IsNotExist(err) {
		t.Errorf("staging dir not cleaned up: %v", err)
	}

	// Re-running is a cheap no-op that doesn't error.
	imp2 := testImporter(t, si, srv)
	if err := imp2.run(context.Background()); err != nil {
		t.Fatalf("second run: %v", err)
	}
}

func TestDumpImportResumesAfterCancel(t *testing.T) {
	srv := serveDumps(t, fixtureSparkTar(t), fixtureCanonicalTarZst(t))

	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, testLogger())
	imp := testImporter(t, si, srv)

	// Cancelled before it can start streaming: no partial state may
	// break the follow-up run.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := imp.run(ctx); err == nil {
		t.Fatal("cancelled run should return an error")
	}

	if err := imp.run(context.Background()); err != nil {
		t.Fatalf("rerun after cancel: %v", err)
	}

	results := si.Search(context.Background(), "hit song", 10)
	if len(results) == 0 {
		t.Fatal("index empty after resumed run")
	}
}

// ---------------------------------------------------------------------------
// Progress reporting
// ---------------------------------------------------------------------------

func TestFormatStreamProgress(t *testing.T) {
	const gb = int64(1) << 30

	tests := []struct {
		name   string
		offset int64
		size   int64
		rate   float64
		want   string
	}{
		{
			name:   "size and rate known",
			offset: 40 * gb,
			size:   200 * gb,
			rate:   20 << 20,
			want:   "40.0 GB / 200.0 GB (20%) · 20.0 MB/s · ~2h16m left",
		},
		{
			name:   "size unknown before first response",
			offset: 2 * gb,
			rate:   10 << 20,
			want:   "2.0 GB downloaded · 10.0 MB/s",
		},
		{
			name:   "rate unknown on the first tick",
			offset: 10 * gb,
			size:   100 * gb,
			want:   "10.0 GB / 100.0 GB (10%)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatStreamProgress(tt.offset, tt.size, tt.rate); got != tt.want {
				t.Errorf("formatStreamProgress() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatETA(t *testing.T) {
	const gb = int64(1) << 30

	tests := []struct {
		name   string
		offset int64
		size   int64
		rate   float64
		want   string
	}{
		{name: "hours", offset: 0, size: 100 * gb, rate: 10 << 20, want: "2h50m"},
		{name: "minutes", offset: 0, size: gb, rate: 10 << 20, want: "1m"},
		{name: "seconds", offset: 0, size: 1 << 20, rate: 10 << 20, want: "<1m"},
		{name: "unknown size", offset: 0, size: -1, rate: 10 << 20, want: ""},
		{name: "stalled", offset: 0, size: 100 * gb, rate: 0, want: ""},
		{name: "past the end", offset: 2 * gb, size: gb, rate: 10 << 20, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatETA(tt.offset, tt.size, tt.rate); got != tt.want {
				t.Errorf("formatETA() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatCount(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{12345, "12,345"},
		{1234567, "1,234,567"},
	}

	for _, tt := range tests {
		if got := formatCount(tt.in); got != tt.want {
			t.Errorf("formatCount(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// The reporter is what keeps the listens stage from looking frozen, so
// it must publish a detail line for the stage while the stream runs.
func TestCountsReporterPublishesDetail(t *testing.T) {
	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, testLogger())
	imp := &dumpImporter{si: si, logger: testLogger()}

	stream := newResumableReader(context.Background(), nil, "", 0)
	stream.offset.Store(50 << 30)
	stream.size.Store(200 << 30)

	rep := &countsReporter{
		imp:        imp,
		stream:     stream,
		lastSample: time.Now().Add(-time.Second),
		lastMoved:  time.Now(),
	}

	rep.tick(time.Now())

	tier := findTier(t, si, dumpStageNames[dumpStageCounts])

	if tier.Completed != 25 {
		t.Errorf("tier completed = %d, want 25", tier.Completed)
	}

	if !strings.Contains(tier.Detail, "50.0 GB / 200.0 GB (25%)") {
		t.Errorf("tier detail = %q, want it to report GB progress", tier.Detail)
	}
}

// A stream that stops moving is reported as stalled rather than as a
// decaying transfer rate.
func TestCountsReporterReportsStall(t *testing.T) {
	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, testLogger())
	imp := &dumpImporter{si: si, logger: testLogger()}

	stream := newResumableReader(context.Background(), nil, "", 0)
	stream.offset.Store(50 << 30)
	stream.size.Store(200 << 30)

	now := time.Now()
	rep := &countsReporter{
		imp:        imp,
		stream:     stream,
		lastSample: now.Add(-countsUIRefreshInterval),
		lastOffset: 50 << 30,
		lastMoved:  now.Add(-2 * countsStallAfter),
	}

	rep.tick(now)

	tier := findTier(t, si, dumpStageNames[dumpStageCounts])

	if !strings.Contains(tier.Detail, "stalled") {
		t.Errorf("tier detail = %q, want a stall notice", tier.Detail)
	}

	if imp.streamRate() != 0 {
		t.Errorf("stalled rate = %v, want 0", imp.streamRate())
	}
}

// A download paused by parser back-pressure is not a network stall and
// must not be reported as one.
func TestCountsReporterDistinguishesBacklog(t *testing.T) {
	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, testLogger())
	imp := &dumpImporter{si: si, logger: testLogger()}

	stream := newResumableReader(context.Background(), nil, "", 0)
	stream.offset.Store(50 << 30)
	stream.size.Store(200 << 30)

	var backlog atomic.Bool

	backlog.Store(true)

	now := time.Now()
	rep := &countsReporter{
		imp:        imp,
		stream:     stream,
		backlog:    &backlog,
		lastSample: now.Add(-countsUIRefreshInterval),
		lastOffset: 50 << 30,
		lastMoved:  now.Add(-2 * countsStallAfter),
	}

	rep.tick(now)

	tier := findTier(t, si, dumpStageNames[dumpStageCounts])

	if strings.Contains(tier.Detail, "stalled") {
		t.Errorf("tier detail = %q, want parsing back-pressure, not a stall", tier.Detail)
	}

	if !strings.Contains(tier.Detail, "parsing") {
		t.Errorf("tier detail = %q, want it to name the parsing pause", tier.Detail)
	}
}

func findTier(t *testing.T, si *SearchIndex, name string) TierStatus {
	t.Helper()

	for _, tier := range si.GetIndexStatus().Tiers {
		if tier.Name == name {
			return tier
		}
	}

	t.Fatalf("tier %q not found", name)

	return TierStatus{}
}

func TestCheckFreeDisk(t *testing.T) {
	dir := t.TempDir()

	if err := checkFreeDisk(dir, 1); err != nil {
		t.Errorf("1 byte requirement should pass: %v", err)
	}

	if err := checkFreeDisk(dir, 1<<62); err == nil {
		t.Error("absurd requirement should fail")
	}
}

func TestDiscoverDumpFilePickNewest(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			_, _ = io.WriteString(w, `
				<a href="musicbrainz-canonical-dump-20260101-080003/">old</a>
				<a href="musicbrainz-canonical-dump-20260615-080003/">new</a>
				<a href="unrelated-dir/">x</a>`)
		case "/musicbrainz-canonical-dump-20260615-080003/":
			_, _ = io.WriteString(w,
				`<a href="musicbrainz-canonical-dump-20260615-080003.tar.zst">f</a>`)
		default:
			http.NotFound(w, r)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	url, err := discoverDumpFile(
		context.Background(), srv.Client(), srv.URL+"/", canonicalDirRe, canonicalFileRe,
	)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}

	want := srv.URL + "/musicbrainz-canonical-dump-20260615-080003/musicbrainz-canonical-dump-20260615-080003.tar.zst"
	if url != want {
		t.Errorf("url = %s, want %s", url, want)
	}
}

func TestListenerCountUpdateDoesNotTouchPopularity(t *testing.T) {
	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, testLogger())

	si.upsertBatch([]SearchIndexResult{{
		EntityType: "recording",
		MBID:       recA,
		Title:      "Hit Song",
		ArtistName: "Solo Star",
		ArtistMBID: artA,
		Popularity: 12,
	}})

	updated := si.updateListenerCounts(map[string]PopularityData{
		recA: {ListenCount: 999_999, ListenerCount: 42},
	})
	if updated != 1 {
		t.Fatalf("updated = %d, want 1", updated)
	}

	rows, err := db.QueryContext(
		"SELECT popularity, listener_count FROM explore_index WHERE mbid = ?", dbMBID(recA),
	)
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		t.Fatal("row missing")
	}

	var pop, listeners int

	if err := rows.Scan(&pop, &listeners); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if pop != 12 {
		t.Errorf("popularity = %d, want 12 (dump scale must stay authoritative)", pop)
	}

	if listeners != 42 {
		t.Errorf("listener_count = %d, want 42", listeners)
	}
}
