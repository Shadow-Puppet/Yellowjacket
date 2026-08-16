package explore

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yellowjacket/backend/database"
)

// artifactRow is one catalog row written into a test artifact.
type artifactRow struct {
	entityType string
	mbid       string
	title      string
	artistName string
	artistMBID string
	popularity int
}

// writeTestArtifact builds an artifact file matching what cmd/indexexport
// produces: catalog columns only, no FTS, no triggers.
func writeTestArtifact(
	t *testing.T, meta map[string]string, rows []artifactRow,
) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "core-index.db")

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open artifact: %v", err)
	}

	defer func() { _ = db.Close() }()

	for _, stmt := range []string{
		`CREATE TABLE explore_index (
			entity_type      TEXT NOT NULL,
			mbid             TEXT NOT NULL,
			title            TEXT NOT NULL,
			artist_name      TEXT NOT NULL,
			artist_mbid      TEXT NOT NULL,
			aliases          TEXT NOT NULL DEFAULT '',
			popularity       INTEGER NOT NULL DEFAULT 0,
			listener_count   INTEGER NOT NULL DEFAULT 0,
			duration         INTEGER NOT NULL DEFAULT 0,
			caa_release_mbid TEXT NOT NULL DEFAULT '',
			release_name     TEXT NOT NULL DEFAULT '',
			primary_type     TEXT NOT NULL DEFAULT '',
			secondary_types  TEXT NOT NULL DEFAULT '',
			release_date     TEXT NOT NULL DEFAULT '',
			artist_type      TEXT NOT NULL DEFAULT '',
			country          TEXT NOT NULL DEFAULT '',
			disambiguation   TEXT NOT NULL DEFAULT '',
			sort_name        TEXT NOT NULL DEFAULT '',
			discog_fetched   INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (mbid)
		) WITHOUT ROWID`,
		`CREATE TABLE artifact_meta (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("create artifact schema: %v", err)
		}
	}

	for k, v := range meta {
		if _, err := db.Exec(
			`INSERT INTO artifact_meta (key, value) VALUES (?, ?)`, k, v,
		); err != nil {
			t.Fatalf("stamp artifact meta: %v", err)
		}
	}

	for _, r := range rows {
		if _, err := db.Exec(`
			INSERT INTO explore_index
				(entity_type, mbid, title, artist_name, artist_mbid, popularity)
			VALUES (?, ?, ?, ?, ?, ?)`,
			r.entityType, r.mbid, r.title, r.artistName, r.artistMBID, r.popularity,
		); err != nil {
			t.Fatalf("insert artifact row: %v", err)
		}
	}

	return path
}

// validMeta is the artifact_meta a well-formed artifact carries.
func validMeta() map[string]string {
	return map[string]string{
		"artifact_version":       supportedArtifactVersion,
		"built_at":               "2026-07-17T03:53:43Z",
		"listens_applied_series": "2593",
		"source_rows":            "2052168",
	}
}

func TestImportCoreArtifactMergesCatalog(t *testing.T) {
	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, testLogger())

	path := writeTestArtifact(t, validMeta(), []artifactRow{
		{"artist", artA, "Artist A", "Artist A", artA, 5000},
		{"artist", artB, "Artist B", "Artist B", artB, 4000},
		{"release_group", rgA, "Album A", "Artist A", artA, 3000},
		{"recording", recA, "Song A", "Artist A", artA, 2000},
	})

	if err := si.importCoreArtifact(context.Background(), path); err != nil {
		t.Fatalf("importCoreArtifact: %v", err)
	}

	var got int
	if err := db.QueryRowWriter(
		"SELECT COUNT(*) FROM explore_index",
	).Scan(&got); err != nil {
		t.Fatalf("count rows: %v", err)
	}

	if got != 4 {
		t.Errorf("merged %d rows, want 4", got)
	}

	// The merge must leave the index searchable: the FTS rebuild that
	// closes the bulk-load window is the only thing populating it, since
	// the triggers were dropped for the duration.
	var hits int
	if err := db.QueryRowWriter(
		`SELECT COUNT(*) FROM explore_index_fts WHERE explore_index_fts MATCH ?`,
		"Song",
	).Scan(&hits); err != nil {
		t.Fatalf("query fts: %v", err)
	}

	if hits == 0 {
		t.Error("FTS index is empty after merge; search would return nothing")
	}
}

func TestImportCoreArtifactStampsMeta(t *testing.T) {
	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, testLogger())

	path := writeTestArtifact(t, validMeta(), []artifactRow{
		{"artist", artA, "Artist A", "Artist A", artA, 5000},
	})

	if err := si.importCoreArtifact(context.Background(), path); err != nil {
		t.Fatalf("importCoreArtifact: %v", err)
	}

	if !si.hasMeta(dumpImportDoneKey) {
		t.Error("dump_import_done not stamped; a full dump import would run anyway")
	}

	// Without this the incremental refresh refuses to run and the
	// shipped popularity never updates again.
	if series, ok := si.metaInt(listensAppliedSeriesKey); !ok || series != 2593 {
		t.Errorf("listens_applied_series = %d (ok=%v), want 2593", series, ok)
	}

	if !si.hasMeta(coreArtifactVersionKey) {
		t.Error("core_artifact_version not stamped")
	}
}

// A merge must never downgrade what the index already holds, because a
// user's own library rows and any lazily-fetched detail predate it.
func TestImportCoreArtifactPreservesLocalData(t *testing.T) {
	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, testLogger())

	si.upsertBatch([]SearchIndexResult{{
		EntityType:    "recording",
		MBID:          recA,
		Title:         "Song A",
		ArtistName:    "Artist A",
		ArtistMBID:    artA,
		Popularity:    9999,
		Duration:      210000,
		InLibrary:     true,
		DiscogFetched: true,
	}})

	path := writeTestArtifact(t, validMeta(), []artifactRow{
		// Lower popularity and no duration: both must lose.
		{"recording", recA, "Song A", "Artist A", artA, 10},
	})

	if err := si.importCoreArtifact(context.Background(), path); err != nil {
		t.Fatalf("importCoreArtifact: %v", err)
	}

	var popularity, duration, inLibrary, discogFetched int

	if err := db.QueryRowWriter(`
		SELECT popularity, duration, in_library, discog_fetched
		FROM explore_index WHERE mbid = ?`, dbMBID(recA),
	).Scan(&popularity, &duration, &inLibrary, &discogFetched); err != nil {
		t.Fatalf("read merged row: %v", err)
	}

	if popularity != 9999 {
		t.Errorf("popularity = %d, want 9999 (higher must win)", popularity)
	}

	if duration != 210000 {
		t.Errorf("duration = %d, want 210000 (artifact carries none)", duration)
	}

	if inLibrary != 1 {
		t.Error("in_library was cleared; the artifact must not touch personal columns")
	}

	if discogFetched != 1 {
		t.Error("discog_fetched was cleared by the merge")
	}
}

// The batch walk is the part most likely to drop or duplicate rows, so
// it is exercised across many batch boundaries rather than one.
func TestImportCoreArtifactBatchWalkCoversAllRows(t *testing.T) {
	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, testLogger())

	// Shrink the batch rather than growing the fixture: what matters is
	// crossing several boundaries, including a short final one.
	original := artifactMergeBatch
	artifactMergeBatch = 100

	t.Cleanup(func() { artifactMergeBatch = original })

	const total = 337

	rows := make([]artifactRow, 0, total)
	for i := range total {
		rows = append(rows, artifactRow{
			entityType: "recording",
			mbid:       syntheticMBID(i),
			title:      "Song",
			artistName: "Artist",
			artistMBID: artA,
			popularity: i,
		})
	}

	path := writeTestArtifact(t, validMeta(), rows)

	if err := si.importCoreArtifact(context.Background(), path); err != nil {
		t.Fatalf("importCoreArtifact: %v", err)
	}

	var got int
	if err := db.QueryRowWriter(
		"SELECT COUNT(*) FROM explore_index",
	).Scan(&got); err != nil {
		t.Fatalf("count rows: %v", err)
	}

	if got != total {
		t.Errorf("merged %d rows, want %d", got, total)
	}
}

func TestImportCoreArtifactRejectsBadArtifacts(t *testing.T) {
	tests := []struct {
		name string
		meta map[string]string
		rows []artifactRow
		want error
	}{
		{
			name: "version mismatch",
			meta: map[string]string{"artifact_version": "99"},
			rows: []artifactRow{{"artist", artA, "A", "A", artA, 1}},
			want: ErrArtifactVersion,
		},
		{
			name: "no version",
			meta: map[string]string{},
			rows: []artifactRow{{"artist", artA, "A", "A", artA, 1}},
			want: ErrArtifactVersion,
		},
		{
			name: "empty catalog",
			meta: validMeta(),
			rows: nil,
			want: ErrArtifactUnusable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := database.NewTestDB(t)
			si := NewSearchIndex(db, nil, nil, testLogger())

			path := writeTestArtifact(t, tt.meta, tt.rows)

			err := si.importCoreArtifact(context.Background(), path)
			if err == nil {
				t.Fatal("expected rejection, got nil")
			}

			if !strings.Contains(err.Error(), tt.want.Error()) {
				t.Errorf("error = %v, want it to wrap %v", err, tt.want)
			}

			// A rejected artifact must not leave the index claiming it
			// has a catalog, or the real build would never run.
			if si.hasMeta(dumpImportDoneKey) {
				t.Error("rejected artifact still stamped dump_import_done")
			}
		})
	}
}

func TestInspectArtifactRejectsNonArtifactFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "garbage.db")

	if err := os.WriteFile(path, []byte("not a database"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if _, err := inspectArtifact(path); err == nil {
		t.Error("expected rejection of a non-artifact file")
	}
}

// syntheticMBID produces distinct, well-formed MBIDs for bulk fixtures.
func syntheticMBID(i int) string {
	const hex = "0123456789abcdef"

	buf := []byte("00000000-0000-0000-0000-000000000000")

	for pos := len(buf) - 1; pos >= 0 && i > 0; pos-- {
		if buf[pos] == '-' {
			continue
		}

		buf[pos] = hex[i%16]
		i /= 16
	}

	return string(buf)
}

// resolveArtistName falls back to the MBID when no name is available.
// That value must never reach the index: the upsert no longer defends
// against it, so the writer is the only thing standing in the way.
func TestAddFromCacheNeverStoresMBIDAsName(t *testing.T) {
	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, testLogger())

	si.AddFromCache(artA, artA, []MBReleaseGroup{
		{MBID: rgA, Title: "Album A", PrimaryType: "Album"},
	})

	var artistRows int
	if err := db.QueryRowWriter(
		`SELECT COUNT(*) FROM explore_index
		 WHERE entity_type = 'artist' AND title = mbid`,
	).Scan(&artistRows); err != nil {
		t.Fatalf("count artist rows: %v", err)
	}

	if artistRows != 0 {
		t.Errorf("%d artist rows stored the MBID as their title", artistRows)
	}

	var artistName string
	if err := db.QueryRowWriter(
		`SELECT artist_name FROM explore_index WHERE mbid = ?`, dbMBID(rgA),
	).Scan(&artistName); err != nil {
		t.Fatalf("read release group: %v", err)
	}

	if artistName == artA {
		t.Error("release group stored the artist MBID as its artist_name")
	}

	// A real name arriving later must still win over the empty one.
	si.AddFromCache("Real Artist", artA, []MBReleaseGroup{
		{MBID: rgA, Title: "Album A", PrimaryType: "Album"},
	})

	if err := db.QueryRowWriter(
		`SELECT artist_name FROM explore_index WHERE mbid = ?`, dbMBID(rgA),
	).Scan(&artistName); err != nil {
		t.Fatalf("re-read release group: %v", err)
	}

	if artistName != "Real Artist" {
		t.Errorf("artist_name = %q, want it filled in once known", artistName)
	}
}

// The importer names the artifact's columns independently of the
// exporter that writes them.  If the two lists drift, the merge either
// fails outright or silently shifts values into the wrong columns, so
// they are compared directly against cmd/indexexport's source.
func TestArtifactColumnsMatchExporter(t *testing.T) {
	src, err := os.ReadFile("../../cmd/indexexport/main.go")
	if err != nil {
		t.Fatalf("read exporter: %v", err)
	}

	const marker = "const catalogColumns = `"

	i := strings.Index(string(src), marker)
	if i < 0 {
		t.Fatalf("catalogColumns not found in cmd/indexexport/main.go")
	}

	rest := string(src)[i+len(marker):]

	j := strings.Index(rest, "`")
	if j < 0 {
		t.Fatal("unterminated catalogColumns literal")
	}

	normalise := func(s string) string {
		out := make([]string, 0, 32)
		for _, f := range strings.Split(s, ",") {
			out = append(out, strings.Join(strings.Fields(f), ""))
		}

		return strings.Join(out, ",")
	}

	exporter := normalise(rest[:j])
	importer := normalise(artifactCatalogColumns)

	if exporter != importer {
		t.Errorf("column lists have drifted:\n exporter: %s\n importer: %s",
			exporter, importer)
	}
}

// TestImportCoreArtifactAcceptsBothEncodings is the compatibility half
// of the storage change.
//
// The catalog stores an MBID as 16 raw bytes and an entity type as a
// code, which took the table and its indexes from 677 MB to 389 MB. A
// published artifact carries whichever form the exporter that built it
// used, and there is one already out there in the older text form — so
// the importer decides by asking the artifact, not by trusting a
// version number, and both must land identically.
func TestImportCoreArtifactAcceptsBothEncodings(t *testing.T) {
	compact := filepath.Join(t.TempDir(), "core-index.db")

	db, err := sql.Open("sqlite", "file:"+compact)
	if err != nil {
		t.Fatalf("open artifact: %v", err)
	}

	if _, err := db.Exec(`CREATE TABLE explore_index (
		entity_type      INTEGER NOT NULL,
		mbid             BLOB NOT NULL,
		title            TEXT NOT NULL,
		artist_name      TEXT NOT NULL,
		artist_mbid      BLOB NOT NULL,
		aliases          TEXT NOT NULL DEFAULT '',
		popularity       INTEGER NOT NULL DEFAULT 0,
		listener_count   INTEGER NOT NULL DEFAULT 0,
		duration         INTEGER NOT NULL DEFAULT 0,
		caa_release_mbid BLOB NOT NULL DEFAULT x'',
		release_name     TEXT NOT NULL DEFAULT '',
		primary_type     TEXT NOT NULL DEFAULT '',
		secondary_types  TEXT NOT NULL DEFAULT '',
		release_date     TEXT NOT NULL DEFAULT '',
		artist_type      TEXT NOT NULL DEFAULT '',
		country          TEXT NOT NULL DEFAULT '',
		disambiguation   TEXT NOT NULL DEFAULT '',
		sort_name        TEXT NOT NULL DEFAULT '',
		discog_fetched   INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (mbid)
	)`); err != nil {
		t.Fatalf("create artifact table: %v", err)
	}

	if _, err := db.Exec(
		`CREATE TABLE artifact_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
	); err != nil {
		t.Fatalf("create artifact meta: %v", err)
	}

	for k, v := range validMeta() {
		if _, err := db.Exec(
			"INSERT INTO artifact_meta (key, value) VALUES (?, ?)", k, v,
		); err != nil {
			t.Fatalf("write artifact meta: %v", err)
		}
	}

	if _, err := db.Exec(`
		INSERT INTO explore_index (entity_type, mbid, title, artist_name, artist_mbid, popularity)
		VALUES (1, ?, 'Artist A', 'Artist A', ?, 5000)`,
		mbidBytes(artA), mbidBytes(artA),
	); err != nil {
		t.Fatalf("write artifact row: %v", err)
	}

	_ = db.Close()

	live := database.NewTestDB(t)
	si := NewSearchIndex(live, nil, nil, testLogger())

	if err := si.importCoreArtifact(context.Background(), compact); err != nil {
		t.Fatalf("importCoreArtifact (compact): %v", err)
	}

	got := si.LookupArtistByMBID(artA)
	if got == nil {
		t.Fatal("artist from a compact artifact was not imported")
	}

	if got.Title != "Artist A" || got.MBID != artA {
		t.Errorf("imported %+v, want Artist A / %s", got, artA)
	}
}

// TestImportCoreArtifactReadsTotalsWhenPresent covers both halves of the
// denominator's arrival: an artifact that carries total_tracks imports
// it, and one built before the column existed still imports at all.
//
// The second half is the one worth a test.  Adding a column to the
// importer's SELECT list is how you break every artifact already
// published - "no such column: total_tracks", on a file nobody can
// re-cut retroactively - so the importer asks the artifact what it has,
// the same way it asks which encoding it uses.
func TestImportCoreArtifactReadsTotalsWhenPresent(t *testing.T) {
	path := writeTestArtifact(t, validMeta(), []artifactRow{
		{"release_group", rgA, "Big Album", "Solo Star", artA, 5000},
	})

	// The exporter writes the column; writeTestArtifact builds the older
	// shape, so add it here rather than changing every other test's
	// fixture to carry a value they do not use.
	artifact, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("reopen artifact: %v", err)
	}

	for _, stmt := range []string{
		`ALTER TABLE explore_index ADD COLUMN total_tracks INTEGER NOT NULL DEFAULT 0`,
		`UPDATE explore_index SET total_tracks = 12`,
	} {
		if _, err := artifact.Exec(stmt); err != nil {
			t.Fatalf("add total_tracks: %v", err)
		}
	}

	_ = artifact.Close()

	live := database.NewTestDB(t)
	si := NewSearchIndex(live, nil, nil, testLogger())

	if err := si.importCoreArtifact(context.Background(), path); err != nil {
		t.Fatalf("importCoreArtifact: %v", err)
	}

	rg := si.LookupReleaseGroupByMBID(rgA)
	if rg == nil {
		t.Fatal("release group was not imported")
	}

	if rg.TotalTracks != 12 {
		t.Errorf("TotalTracks = %d, want 12", rg.TotalTracks)
	}

	// And the shape that predates the column: the same import, from an
	// artifact that has no total_tracks at all.
	older := writeTestArtifact(t, validMeta(), []artifactRow{
		{"release_group", rgB, "Duet Album", "Solo Star", artA, 4000},
	})

	live2 := database.NewTestDB(t)
	si2 := NewSearchIndex(live2, nil, nil, testLogger())

	if err := si2.importCoreArtifact(context.Background(), older); err != nil {
		t.Fatalf("importCoreArtifact (no total_tracks column): %v", err)
	}

	old := si2.LookupReleaseGroupByMBID(rgB)
	if old == nil {
		t.Fatal("release group from a column-less artifact was not imported")
	}

	if old.TotalTracks != 0 {
		t.Errorf("TotalTracks = %d, want 0 (the catalog does not say)", old.TotalTracks)
	}
}
