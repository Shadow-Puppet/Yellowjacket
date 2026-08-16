// Command indexexport turns a fully built explore index into the
// compact "core" artifact that ships to users.
//
// The full dump-built index is far too large to distribute (~900MB by
// current budget estimates). The core artifact keeps the most-listened
// artists and their discography slice — enough for Explore to be useful
// on a fresh install — and leaves the long tail to the existing lazy
// per-artist fetch paths.
//
// The artifact deliberately contains no FTS table. The importing client
// inserts these rows into its own explore_index, whose AFTER INSERT
// trigger populates explore_index_fts as a side effect, so shipping a
// search index would be redundant weight.
//
// Usage:
//
//	YJ_HOME=/var/cache/yellowjacket-index indexexport -o core-index.db
package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	_ "modernc.org/sqlite"

	"yellowjacket/backend/system"
)

// Columns copied into the artifact: the global catalog only.
//
// Deliberately excluded are the per-user columns — in_library,
// is_similar, local_artist_id, local_release_group_id,
// local_recording_id — which describe one person's library and are
// recomputed locally by PopulateLocalCrossReferences after import.
const catalogColumns = `entity_type, mbid, title, artist_name, artist_mbid,
	aliases, popularity, listener_count, duration, caa_release_mbid,
	release_name, primary_type, secondary_types, release_date, total_tracks,
	artist_type, country, disambiguation, sort_name, discog_fetched`

var errNoHome = errors.New(
	"YJ_HOME must be set to the directory holding the built index",
)

var errEmptyIndex = errors.New(
	"source index has no rows — run indexbuild to completion first",
)

func main() {
	out := flag.String("o", "core-index.db", "output artifact path")
	artists := flag.Int("artists", 50_000,
		"number of top artists (by listen count) to include")
	perArtistRGs := flag.Int("rgs-per-artist", 15,
		"max release groups per included artist")
	perArtistRecs := flag.Int("recs-per-artist", 30,
		"max recordings per included artist")

	flag.Parse()

	if err := run(*out, *artists, *perArtistRGs, *perArtistRecs); err != nil {
		fmt.Fprintln(os.Stderr, "indexexport:", err)
		os.Exit(1)
	}
}

func run(out string, artists, perArtistRGs, perArtistRecs int) error {
	if os.Getenv("YJ_HOME") == "" {
		return errNoHome
	}

	dataDir, err := system.GetUserDataDirPath()
	if err != nil {
		return fmt.Errorf("resolve data dir: %w", err)
	}

	srcPath := filepath.Join(dataDir, "yj.db")

	// Read-only so an export can never disturb a build that is still
	// running against the same working directory.
	db, err := sql.Open("sqlite",
		"file:"+srcPath+"?_pragma=busy_timeout(10000)&mode=ro")
	if err != nil {
		return fmt.Errorf("open source index: %w", err)
	}

	defer func() { _ = db.Close() }()

	var srcRows int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM explore_index",
	).Scan(&srcRows); err != nil {
		return fmt.Errorf("count source rows: %w", err)
	}

	if srcRows == 0 {
		return errEmptyIndex
	}

	fmt.Printf("source: %s (%d rows)\n", srcPath, srcRows)

	if err := os.Remove(out); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear output: %w", err)
	}

	if _, err := db.Exec(`ATTACH DATABASE ? AS core`, out); err != nil {
		return fmt.Errorf("attach output: %w", err)
	}

	if err := createSchema(db); err != nil {
		return err
	}

	if err := copyRows(db, artists, perArtistRGs, perArtistRecs); err != nil {
		return err
	}

	if err := stampMeta(db, srcRows); err != nil {
		return err
	}

	// DETACH before VACUUM: sqlite cannot vacuum an attached database.
	if _, err := db.Exec(`DETACH DATABASE core`); err != nil {
		return fmt.Errorf("detach output: %w", err)
	}

	if err := vacuum(out); err != nil {
		return err
	}

	return report(out)
}

// createSchema builds the artifact's tables. No FTS and no triggers —
// the importing client's own trigger rebuilds its FTS on insert.
func createSchema(db *sql.DB) error {
	stmts := []string{
		// Column types mirror the app's own explore_index, because the
		// export is a straight copy: MBIDs as 16 raw bytes, entity
		// types as codes.  An importer that meets the older text form
		// converts it (artifactSelectColumns), so this is a size change
		// and not a compatibility break.
		`CREATE TABLE core.explore_index (
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
			total_tracks     INTEGER NOT NULL DEFAULT 0,
			artist_type      TEXT NOT NULL DEFAULT '',
			country          TEXT NOT NULL DEFAULT '',
			disambiguation   TEXT NOT NULL DEFAULT '',
			sort_name        TEXT NOT NULL DEFAULT '',
			discog_fetched   INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (mbid)
		) WITHOUT ROWID`,
		`CREATE TABLE core.artifact_meta (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("create artifact schema: %w", err)
		}
	}

	return nil
}

// copyRows selects the core subset: the top artists by listen count,
// then a bounded slice of each one's release groups and recordings.
//
// The per-artist window mirrors the S2 coverage already in
// dumpcatalog.go — a flat global top-N would give a handful of
// superstars everything and everyone else nothing.
func copyRows(db *sql.DB, artists, perArtistRGs, perArtistRecs int) error {
	if _, err := db.Exec(`
		CREATE TEMP TABLE core_artists AS
		SELECT mbid FROM main.explore_index
		WHERE entity_type = 1 /* artist */
		ORDER BY popularity DESC
		LIMIT ?`, artists,
	); err != nil {
		return fmt.Errorf("select core artists: %w", err)
	}

	copied, err := insertSelect(db, `
		INSERT INTO core.explore_index (`+catalogColumns+`)
		SELECT `+catalogColumns+`
		FROM main.explore_index
		WHERE entity_type = 1 /* artist */
		  AND mbid IN (SELECT mbid FROM core_artists)`)
	if err != nil {
		return err
	}

	fmt.Printf("  artists:        %d\n", copied)

	// The entity codes are the catalog's storage form; see
	// backend/explore/mbid.go.  The exporter copies the local index's
	// encoding through unchanged, so the artifact carries it too - and
	// the importer accepts either, so an artifact built before this
	// still imports.
	for _, sel := range []struct {
		label  string
		entity int
		limit  int
	}{
		{"release groups", 2 /* release_group */, perArtistRGs},
		{"recordings", 3 /* recording */, perArtistRecs},
	} {
		// The window is over artist_mbid so each artist contributes at
		// most `limit` rows, ranked by their own listen counts.
		n, err := insertSelect(db, `
			INSERT INTO core.explore_index (`+catalogColumns+`)
			SELECT `+catalogColumns+` FROM (
				SELECT *, ROW_NUMBER() OVER (
					PARTITION BY artist_mbid ORDER BY popularity DESC
				) AS rn
				FROM main.explore_index
				WHERE entity_type = ?
				  AND artist_mbid IN (SELECT mbid FROM core_artists)
			) WHERE rn <= ?`, sel.entity, sel.limit)
		if err != nil {
			return err
		}

		fmt.Printf("  %-15s %d\n", sel.label+":", n)
	}

	return nil
}

func insertSelect(db *sql.DB, query string, args ...any) (int64, error) {
	res, err := db.Exec(query, args...)
	if err != nil {
		return 0, fmt.Errorf("copy rows: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}

	return n, nil
}

// stampMeta records what the importing client needs to know: that the
// catalog half is already populated, and which incremental listens
// series the popularity numbers are baselined on, so the incremental
// refresh resumes from the right point instead of reapplying deltas.
func stampMeta(db *sql.DB, srcRows int) error {
	series := lookupMeta(db, "listens_applied_series")
	built := lookupMeta(db, "dump_import_done")

	if built == "" {
		built = time.Now().UTC().Format(time.RFC3339)
	}

	entries := map[string]string{
		"artifact_version":       "1",
		"built_at":               built,
		"source_rows":            strconv.Itoa(srcRows),
		"listens_applied_series": series,
	}

	for k, v := range entries {
		if _, err := db.Exec(
			`INSERT OR REPLACE INTO core.artifact_meta (key, value) VALUES (?, ?)`,
			k, v,
		); err != nil {
			return fmt.Errorf("stamp %s: %w", k, err)
		}
	}

	return nil
}

func lookupMeta(db *sql.DB, key string) string {
	var value string

	row := db.QueryRow(
		`SELECT value FROM main.explore_index_meta WHERE key = ?`, key)
	if err := row.Scan(&value); err != nil {
		return ""
	}

	return value
}

func vacuum(path string) error {
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		return fmt.Errorf("reopen artifact: %w", err)
	}

	defer func() { _ = db.Close() }()

	if _, err := db.Exec("VACUUM"); err != nil {
		return fmt.Errorf("vacuum artifact: %w", err)
	}

	return nil
}

func report(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat artifact: %w", err)
	}

	fmt.Printf("\nartifact: %s (%.1f MB)\n",
		path, float64(fi.Size())/(1<<20))
	fmt.Println("compress with: zstd -19 -T0", path)

	return nil
}
