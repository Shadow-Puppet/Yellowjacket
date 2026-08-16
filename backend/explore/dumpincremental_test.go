package explore

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"yellowjacket/backend/database"
)

func TestParseDumpSeries(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"https://x/fullexport/listenbrainz-dump-2593-20260712-000004-full/y.tar", 2593, true},
		{"listenbrainz-dump-2594-20260713-000003-incremental", 2594, true},
		{"listenbrainz-spark-dump-2603-20260722-000003-incremental.tar", 2603, true},
		{"nothing-here", 0, false},
	}

	for _, c := range cases {
		got, ok := parseDumpSeries(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseDumpSeries(%q) = (%d, %v); want (%d, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func popularityOf(t *testing.T, db *database.DB, mbid string) (int, bool) {
	t.Helper()

	rows, err := db.QueryContext(
		"SELECT popularity FROM explore_index WHERE mbid = ?", dbMBID(mbid),
	)
	if err != nil {
		t.Fatalf("query popularity: %v", err)
	}

	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return 0, false
	}

	var pop int
	if err := rows.Scan(&pop); err != nil {
		t.Fatalf("scan popularity: %v", err)
	}

	return pop, true
}

// applyTar runs the incremental split → rollup → commit path against an
// in-memory tar, mirroring applyIncremental without the HTTP stream.
func applyTar(t *testing.T, si *SearchIndex, series int, tarBytes []byte) {
	t.Helper()

	counts, err := aggregateTarListens(context.Background(), bytes.NewReader(tarBytes))
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}

	rec, art, rel := splitCountsByKind(counts)
	rg := si.rollupReleaseDeltas(rel)

	if err := si.commitListenDeltas(series, rec, art, rg); err != nil {
		t.Fatalf("commit series %d: %v", series, err)
	}
}

func TestIncrementalApplyAdditive(t *testing.T) {
	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, testLogger())

	si.upsertBatch([]SearchIndexResult{
		{EntityType: "recording", MBID: recA, Title: "Rec A", Popularity: 100},
		{EntityType: "artist", MBID: artA, Title: "Art A", Popularity: 100},
		{EntityType: "release_group", MBID: rgA, Title: "RG A", Popularity: 100},
	})

	if _, err := db.ExecContext(
		"INSERT INTO release_to_rg (release_mbid, rg_mbid) VALUES (?, ?)", relA, rgA,
	); err != nil {
		t.Fatalf("seed release_to_rg: %v", err)
	}

	// 5 listens of recA on relA credited to artA.
	tarBytes := makeTar(t,
		map[string][]byte{
			"listens/1.parquet": makeParquet(t, listensOf(5, recA, relA, []string{artA})),
		},
		[]string{"listens/1.parquet"},
	)

	applyTar(t, si, 2, tarBytes)

	for _, c := range []struct {
		mbid string
		want int
	}{{recA, 105}, {artA, 105}, {rgA, 105}} {
		if got, ok := popularityOf(t, db, c.mbid); !ok || got != c.want {
			t.Errorf("popularity(%s) = %d (present=%v); want %d", c.mbid, got, ok, c.want)
		}
	}

	if hwm, ok := si.metaInt(listensAppliedSeriesKey); !ok || hwm != 2 {
		t.Errorf("high-water-mark = %d (present=%v); want 2", hwm, ok)
	}

	// A second, later dump accumulates additively.
	applyTar(t, si, 3, tarBytes)

	if got, _ := popularityOf(t, db, recA); got != 110 {
		t.Errorf("popularity(recA) after second dump = %d; want 110", got)
	}

	if hwm, _ := si.metaInt(listensAppliedSeriesKey); hwm != 3 {
		t.Errorf("high-water-mark after second dump = %d; want 3", hwm)
	}
}

func TestIncrementalIgnoresUnknownAndUnmapped(t *testing.T) {
	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, testLogger())

	// Only recA is indexed; relA has no release_to_rg mapping.
	si.upsertBatch([]SearchIndexResult{
		{EntityType: "recording", MBID: recA, Title: "Rec A", Popularity: 100},
	})

	// Listens for recB (not indexed) on relB (unmapped) credited to
	// artB (not indexed).  Nothing should change and no row created.
	tarBytes := makeTar(t,
		map[string][]byte{
			"listens/1.parquet": makeParquet(t, listensOf(7, recB, relB, []string{artB})),
		},
		[]string{"listens/1.parquet"},
	)

	applyTar(t, si, 5, tarBytes)

	if _, ok := popularityOf(t, db, recB); ok {
		t.Error("recB should not have been inserted into the index")
	}

	if got, _ := popularityOf(t, db, recA); got != 100 {
		t.Errorf("popularity(recA) = %d; want 100 (untouched)", got)
	}
}

func TestDiscoverIncrementalDumps(t *testing.T) {
	series := []string{"2594", "2595", "2596"}

	mux := http.NewServeMux()

	mux.HandleFunc("/incremental/", func(w http.ResponseWriter, r *http.Request) {
		// Only the base listing (exact path); dir listings are handled
		// by their own more-specific patterns below.
		if r.URL.Path != "/incremental/" {
			http.NotFound(w, r)

			return
		}

		for _, s := range series {
			_, _ = fmt.Fprintf(w,
				`<a href="listenbrainz-dump-%s-20260713-000003-incremental/">dir</a>`+"\n", s,
			)
		}
	})

	for _, s := range series {
		dir := fmt.Sprintf("/incremental/listenbrainz-dump-%s-20260713-000003-incremental/", s)
		file := fmt.Sprintf("listenbrainz-spark-dump-%s-20260713-000003-incremental.tar", s)

		mux.HandleFunc(dir, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprintf(w, `<a href="%s">file</a>`, file)
		})
	}

	srv := httptest.NewServer(mux)
	defer srv.Close()

	dumps, err := discoverIncrementalDumps(
		context.Background(), srv.Client(), srv.URL+"/incremental/", 2594,
	)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}

	// Only 2595 and 2596 are newer than the 2594 high-water-mark.
	if len(dumps) != 2 {
		t.Fatalf("got %d dumps; want 2: %+v", len(dumps), dumps)
	}

	if dumps[0].series != 2595 || dumps[1].series != 2596 {
		t.Errorf("series order wrong: %d, %d; want 2595, 2596", dumps[0].series, dumps[1].series)
	}

	if !strings.Contains(dumps[0].url, "spark-dump-2595") {
		t.Errorf("unexpected url: %s", dumps[0].url)
	}
}
