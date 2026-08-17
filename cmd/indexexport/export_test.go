//go:build indexbuild

package main

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// The columns an index built before the completeness work has: every
// current one except total_tracks.
//
// Filtered rather than string-replaced, because the list is formatted
// across lines: `strings.Replace(catalogColumns, "total_tracks, ", …)`
// matches nothing (the name is followed by a newline, not a space) and
// silently yields the *current* list -- so the test built a modern
// source index and proved nothing while passing its own premise.
var oldColumns = withoutTotals(catalogColumns)

func withoutTotals(cols string) string {
	kept := make([]string, 0, 20)

	for _, part := range strings.Split(cols, ",") {
		if strings.TrimSpace(part) == "total_tracks" {
			continue
		}

		kept = append(kept, strings.TrimSpace(part))
	}

	return strings.Join(kept, ", ")
}

// TestExportFromAnIndexWithoutTotals reproduces the failure that broke
// the index-artifact job, symptom first.
//
// The job's /cache volume is a real YJ_HOME that survives between runs
// and holds ~205 GB, so its explore_index is Cache and is deliberately
// not dropped by cmd/indexbuild's schema repair -- which means a column
// added to the schema afterwards is simply absent from it. The exporter
// selected it anyway and the whole run died with
//
//	indexexport: copy rows: SQL logic error: no such column: total_tracks
//
// after three minutes of work, on a job that publishes the catalog
// every user downloads.
func TestExportFromAnIndexWithoutTotals(t *testing.T) {
	t.Parallel()

	db := openWithSource(t, oldColumns)

	if got := sourceColumns(db); strings.Contains(got, "total_tracks") {
		t.Fatalf("source list still names total_tracks: %s", got)
	}

	if err := copyRows(db, 10, 5, 5); err != nil {
		t.Fatalf("export from an index without total_tracks: %v", err)
	}

	// Zero, not absent: the artifact keeps every column so an importer
	// needs no second shape, and 0 is what the column already means by
	// "the catalog does not say".
	var total int
	if err := db.QueryRow(
		`SELECT total_tracks FROM core.explore_index WHERE entity_type = 2`,
	).Scan(&total); err != nil {
		t.Fatalf("read exported total_tracks: %v", err)
	}

	if total != 0 {
		t.Errorf("total_tracks = %d, want 0", total)
	}
}

// TestExportCarriesTotalsWhenTheIndexHasThem is the other half: the
// probe must not cost the totals of an index that does have them.
func TestExportCarriesTotalsWhenTheIndexHasThem(t *testing.T) {
	t.Parallel()

	db := openWithSource(t, catalogColumns)

	if got := sourceColumns(db); !strings.Contains(got, "total_tracks") {
		t.Fatalf("source list dropped total_tracks: %s", got)
	}

	if err := copyRows(db, 10, 5, 5); err != nil {
		t.Fatalf("export: %v", err)
	}

	var total int
	if err := db.QueryRow(
		`SELECT total_tracks FROM core.explore_index WHERE entity_type = 2`,
	).Scan(&total); err != nil {
		t.Fatalf("read exported total_tracks: %v", err)
	}

	if total != 12 {
		t.Errorf("total_tracks = %d, want 12", total)
	}
}

// openWithSource builds a source index carrying exactly `columns`, with
// one artist and one of its release groups, and attaches a fresh
// artifact database as `core`.
func openWithSource(t *testing.T, columns string) *sql.DB {
	t.Helper()

	dir := t.TempDir()

	db, err := sql.Open("sqlite", filepath.Join(dir, "src.db"))
	if err != nil {
		t.Fatalf("open source: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	// The source's shape is the point of the test, so it is spelled
	// out here rather than taken from the app's schema, which is
	// always current by definition.
	create := `CREATE TABLE explore_index (
		id INTEGER PRIMARY KEY,
		entity_type INTEGER NOT NULL,
		mbid BLOB NOT NULL,
		title TEXT NOT NULL DEFAULT '',
		artist_name TEXT NOT NULL DEFAULT '',
		artist_mbid BLOB NOT NULL DEFAULT x'',
		aliases TEXT NOT NULL DEFAULT '',
		popularity INTEGER NOT NULL DEFAULT 0,
		listener_count INTEGER NOT NULL DEFAULT 0,
		duration INTEGER NOT NULL DEFAULT 0,
		caa_release_mbid BLOB NOT NULL DEFAULT x'',
		release_name TEXT NOT NULL DEFAULT '',
		primary_type TEXT NOT NULL DEFAULT '',
		secondary_types TEXT NOT NULL DEFAULT '',
		release_date TEXT NOT NULL DEFAULT '',
		total_tracks INTEGER NOT NULL DEFAULT 0,
		artist_type TEXT NOT NULL DEFAULT '',
		country TEXT NOT NULL DEFAULT '',
		disambiguation TEXT NOT NULL DEFAULT '',
		sort_name TEXT NOT NULL DEFAULT '',
		discog_fetched INTEGER NOT NULL DEFAULT 0
	)`

	if !strings.Contains(columns, "total_tracks") {
		create = strings.Replace(
			create, "total_tracks INTEGER NOT NULL DEFAULT 0,\n", "", 1,
		)
	}

	if _, err := db.Exec(create); err != nil {
		t.Fatalf("create source: %v", err)
	}

	seed := `INSERT INTO explore_index (` + columns + `) VALUES `

	if strings.Contains(columns, "total_tracks") {
		seed += `(1, x'00000000000000000000000000000001', 'A', 'A',
			x'00000000000000000000000000000001', '', 100, 100, 0, x'',
			'', '', '', '', 12, '', '', '', '', 0),
			(2, x'00000000000000000000000000000002', 'RG', 'A',
			x'00000000000000000000000000000001', '', 90, 90, 0, x'',
			'', 'Album', '', '', 12, '', '', '', '', 0)`
	} else {
		seed += `(1, x'00000000000000000000000000000001', 'A', 'A',
			x'00000000000000000000000000000001', '', 100, 100, 0, x'',
			'', '', '', '', '', '', '', '', 0),
			(2, x'00000000000000000000000000000002', 'RG', 'A',
			x'00000000000000000000000000000001', '', 90, 90, 0, x'',
			'', 'Album', '', '', '', '', '', '', 0)`
	}

	if _, err := db.Exec(seed); err != nil {
		t.Fatalf("seed source: %v", err)
	}

	if _, err := db.Exec(
		`ATTACH DATABASE ? AS core`, filepath.Join(dir, "core.db"),
	); err != nil {
		t.Fatalf("attach core: %v", err)
	}

	if err := createSchema(db); err != nil {
		t.Fatalf("create artifact schema: %v", err)
	}

	return db
}
