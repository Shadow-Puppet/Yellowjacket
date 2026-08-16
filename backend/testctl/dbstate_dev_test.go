//go:build dev

package testctl

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"yellowjacket/backend/database"
)

// newDeps wires the control surface to an in-memory database and a
// throwaway YJ_HOME, so snapshots land somewhere the test owns.
func newDeps(t *testing.T) Deps {
	t.Helper()

	t.Setenv("YJ_HOME", t.TempDir())

	return Deps{
		Logger:  slog.New(slog.DiscardHandler),
		DB:      database.NewTestDB(t),
		Context: context.Background,
	}
}

// TestRestoreRoundTrip is the regression test for the failure this
// endpoint shipped with first: copying tables in *name* order deletes a
// parent row whose ON DELETE CASCADE then wipes a child table already
// restored earlier in the loop, and the commit fails with a bare
// "FOREIGN KEY constraint failed (787)" naming nothing.  Deferring the
// check is not enough — the cascade still fires — so the copy runs with
// foreign keys off and is verified afterwards.
func TestRestoreRoundTrip(t *testing.T) {
	// No t.Parallel: newDeps uses t.Setenv (YJ_HOME), which the testing
	// package forbids in parallel tests because the environment is
	// process-wide.
	d := newDeps(t)

	if _, err := d.DB.ExecContext(
		`INSERT INTO libraries (id, name, path) VALUES (1, 'fixtures', '/music')`,
	); err != nil {
		t.Fatalf("seed library: %v", err)
	}

	database.InsertTestTrack(t, d.DB, database.TestTrack{
		FilePath:  "/music/a.mp3",
		Title:     "A",
		Artist:    "Fixture Artist",
		LengthMs:  2000,
		LibraryID: 1,
	})

	snapReq := httptest.NewRequest(http.MethodPost, "/__test/db/snapshot?name=unit", nil)

	if _, err := handleSnapshot(d, snapReq); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	if _, err := d.DB.ExecContext(`DELETE FROM audio_files`); err != nil {
		t.Fatalf("mutate: %v", err)
	}

	if got := countTracks(t, d); got != 0 {
		t.Fatalf("after delete: got %d tracks, want 0", got)
	}

	restoreReq := httptest.NewRequest(http.MethodPost, "/__test/db/restore?name=unit", nil)

	if _, err := handleRestore(d, restoreReq); err != nil {
		t.Fatalf("restore: %v", err)
	}

	if got := countTracks(t, d); got != 1 {
		t.Fatalf("after restore: got %d tracks, want 1", got)
	}

	// Enforcement must be back on afterwards; leaving it off would let
	// every later test — and the running app — write garbage silently.
	var fk int
	if err := d.DB.QueryRowWriter("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("read pragma: %v", err)
	}

	if fk != 1 {
		t.Fatal("foreign keys left disabled after restore")
	}
}

// TestRestorableTablesSkipsFTSInternals guards the other half of the
// copy: FTS5 virtual tables cannot be written with SELECT *, and their
// shadow tables (_data, _idx, _docsize, _config) are storage details
// that must be rebuilt rather than copied.
func TestRestorableTablesSkipsFTSInternals(t *testing.T) {
	tables, err := restorableTables(newDeps(t))
	if err != nil {
		t.Fatalf("restorableTables: %v", err)
	}

	if len(tables) == 0 {
		t.Fatal("no restorable tables found")
	}

	for _, name := range tables {
		switch name {
		case "search_index", "lyrics_index", "explore_index_fts",
			"explore_champion_fts":
			t.Errorf("virtual table %q must not be copied", name)
		case "search_index_data", "lyrics_index_idx",
			"explore_index_fts_config":
			t.Errorf("shadow table %q must not be copied", name)
		}
	}

	// explore_index is an ordinary table whose name is a prefix of two
	// virtual ones; excluding it would silently drop the catalog.
	if !contains(tables, "explore_index") {
		t.Error("explore_index was wrongly treated as an FTS internal")
	}
}

func TestSnapshotNameIsValidated(t *testing.T) {
	d := newDeps(t)

	for _, name := range []string{"", "../escape", "has space", "a/b"} {
		req := httptest.NewRequest(
			http.MethodPost,
			"/__test/db/snapshot?name="+url.QueryEscape(name),
			nil,
		)

		if _, err := handleSnapshot(d, req); err == nil {
			t.Errorf("snapshot(%q) was accepted", name)
		}
	}
}

// TestRegisterRequiresOptIn pins the second gate: a dev build alone must
// not expose the surface, because `make dev` is something a human runs.
func TestRegisterRequiresOptIn(t *testing.T) {
	_ = os.Unsetenv(EnvEnable)

	var r recordingRegistrar

	Register(&r, Deps{Logger: slog.New(slog.DiscardHandler)})

	if len(r.patterns) != 0 {
		t.Fatalf("registered %v without %s=1", r.patterns, EnvEnable)
	}

	t.Setenv(EnvEnable, "1")
	Register(&r, Deps{Logger: slog.New(slog.DiscardHandler)})

	if len(r.patterns) != 1 || r.patterns[0] != Prefix {
		t.Fatalf("got patterns %v, want [%s]", r.patterns, Prefix)
	}
}

// recordingRegistrar stands in for *assets.Handler and remembers what
// was mounted.
type recordingRegistrar struct {
	patterns []string
}

func (r *recordingRegistrar) RegisterHandler(pattern string, _ http.Handler) {
	r.patterns = append(r.patterns, pattern)
}

func countTracks(t *testing.T, d Deps) int {
	t.Helper()

	var n int
	if err := d.DB.QueryRowWriter(
		"SELECT COUNT(*) FROM audio_files",
	).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}

	return n
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}

	return false
}
