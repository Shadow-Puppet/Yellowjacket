package library

import (
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"yellowjacket/backend/database"
	"yellowjacket/backend/database/sql/sqlcgen"
	"yellowjacket/backend/events"
	"yellowjacket/internal/testfixtures"
)

// setupScanLibrary builds a Library over a temp directory holding
// copies of `count` real fixture tracks, plus the libraries row the
// scan needs.  Real files, because the scan extracts tags from them.
func setupScanLibrary(
	t *testing.T,
	count int,
) (lib *Library, dir string, paths []string, rec *events.Recorder, libID int64) {
	t.Helper()

	m := testfixtures.Load(t)
	sources := m.Case(t, testfixtures.CaseFLACAlbum)

	if len(sources) < count {
		t.Fatalf("fixture case has %d tracks, need %d", len(sources), count)
	}

	dir = t.TempDir()
	paths = make([]string, 0, count)

	for _, src := range sources[:count] {
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read fixture %s: %v", src, err)
		}

		dst := filepath.Join(dir, filepath.Base(src))
		if err := os.WriteFile(dst, data, 0o600); err != nil {
			t.Fatalf("write fixture copy: %v", err)
		}

		paths = append(paths, dst)
	}

	db := database.NewTestDB(t)
	rec = events.NewRecorder()

	lib = &Library{
		ctx:    events.WithSink(t.Context(), rec),
		logger: slog.Default(),
		conf:   &Config{},
		db:     db,
	}

	row, err := db.Queries.CreateLibrary(t.Context(), sqlcgen.CreateLibraryParams{
		Name: "Test",
		Path: dir,
	})
	if err != nil {
		t.Fatalf("create library row: %v", err)
	}

	slices.Sort(paths)

	return lib, dir, paths, rec, row.ID
}

// scannedPaths returns the file paths currently in the database, sorted.
func scannedPaths(t *testing.T, lib *Library) []string {
	t.Helper()

	rows, err := lib.db.Queries.GetAllAudioFilePaths(t.Context())
	if err != nil {
		t.Fatalf("read audio file paths: %v", err)
	}

	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.FilePath)
	}

	slices.Sort(out)

	return out
}

// TestRemoveFromLibrary_SurvivesRescan is the assertion the whole phase
// rests on: a removed path stays removed across a real scan of the real
// directory, and the file it named is still on disk.
//
// Its positive half is not optional.  A guard that excluded everything
// would satisfy "the removed path did not come back" for free, so the
// same scan must also put back a row deleted *without* an exclusion.
func TestRemoveFromLibrary_SurvivesRescan(t *testing.T) {
	t.Parallel()

	lib, dir, paths, _, libID := setupScanLibrary(t, 3)

	lib.scanInternal(libID, "Test", dir)

	if got := scannedPaths(t, lib); len(got) != 3 {
		t.Fatalf("first scan imported %d tracks, want 3: %v", len(got), got)
	}

	removed := paths[0]
	// The control: its row is deleted directly, with no exclusion, so
	// the same scan has to bring it back.
	control := paths[1]

	result, err := lib.RemoveFromLibrary([]string{removed})
	if err != nil {
		t.Fatalf("RemoveFromLibrary: %v", err)
	}

	if result.TracksRemoved != 1 || result.PathsExcluded != 1 {
		t.Fatalf(
			"RemoveFromLibrary = %+v, want 1 removed and 1 excluded",
			result,
		)
	}

	controlRow, err := lib.db.Queries.GetAudioFileByPath(t.Context(), control)
	if err != nil {
		t.Fatalf("look up control track: %v", err)
	}

	if err := lib.db.Queries.DeleteAudioFile(t.Context(), controlRow.ID); err != nil {
		t.Fatalf("delete control row: %v", err)
	}

	lib.scanInternal(libID, "Test", dir)

	after := scannedPaths(t, lib)

	if slices.Contains(after, removed) {
		t.Errorf("excluded path came back after a rescan: %s\nrows: %v", removed, after)
	}

	if !slices.Contains(after, control) {
		t.Errorf(
			"the rescan did not re-import a path that was NOT excluded (%s)"+
				" — the exclusion is skipping more than it was asked to\nrows: %v",
			control, after,
		)
	}

	// The promise the confirmation dialog makes.
	if _, err := os.Stat(removed); err != nil {
		t.Errorf("removed file is no longer on disk: %v", err)
	}
}

// TestRemoveFromLibrary_SoftScanSeesNoChange pins the trap that would
// otherwise queue a full scan on every launch: the soft scan compares
// the number of audio files on disk against the number of rows, and an
// excluded path is on disk and deliberately not a row.
func TestRemoveFromLibrary_SoftScanSeesNoChange(t *testing.T) {
	t.Parallel()

	lib, dir, paths, _, libID := setupScanLibrary(t, 3)

	lib.scanInternal(libID, "Test", dir)

	if _, err := lib.RemoveFromLibrary([]string{paths[0]}); err != nil {
		t.Fatalf("RemoveFromLibrary: %v", err)
	}

	dbCount, err := lib.db.Queries.CountAudioFiles(t.Context(), libID)
	if err != nil {
		t.Fatalf("count rows: %v", err)
	}

	diskCount, _ := surveyAudioFiles(dir, lib.excludedPathSet(libID))

	if diskCount != dbCount {
		t.Errorf(
			"soft scan would see disk %d vs db %d — every launch queues a full scan",
			diskCount, dbCount,
		)
	}

	// And the positive half: without the exclusion set the survey still
	// counts the file, which is what makes the argument above real
	// rather than a tautology about a function that counts nothing.
	if raw, _ := surveyAudioFiles(dir, nil); raw != dbCount+1 {
		t.Errorf("unfiltered survey = %d, want %d", raw, dbCount+1)
	}
}

// TestRemoveFromLibrary_FullRescanClearsExclusions pins the only route
// back for a path removed by mistake.
func TestRemoveFromLibrary_FullRescanClearsExclusions(t *testing.T) {
	t.Parallel()

	lib, dir, paths, _, libID := setupScanLibrary(t, 2)

	lib.scanInternal(libID, "Test", dir)

	if _, err := lib.RemoveFromLibrary([]string{paths[0]}); err != nil {
		t.Fatalf("RemoveFromLibrary: %v", err)
	}

	if err := lib.clearLibraryTables(); err != nil {
		t.Fatalf("clearLibraryTables: %v", err)
	}

	count, err := lib.db.Queries.CountExcludedPathsByLibrary(t.Context(), libID)
	if err != nil {
		t.Fatalf("count exclusions: %v", err)
	}

	if count != 0 {
		t.Fatalf("full rescan left %d exclusions behind", count)
	}

	lib.scanInternal(libID, "Test", dir)

	if !slices.Contains(scannedPaths(t, lib), paths[0]) {
		t.Error("a full rescan did not bring back a previously excluded path")
	}
}

// TestRemoveFromLibrary_EmitsPatchablePayload checks the event carries
// what a store needs to patch rather than invalidate.
func TestRemoveFromLibrary_EmitsPatchablePayload(t *testing.T) {
	t.Parallel()

	lib, dir, paths, rec, libID := setupScanLibrary(t, 2)

	lib.scanInternal(libID, "Test", dir)

	if _, err := lib.RemoveFromLibrary([]string{paths[0]}); err != nil {
		t.Fatalf("RemoveFromLibrary: %v", err)
	}

	ev, ok := rec.Last(events.TracksRemovedFromLibrary)
	if !ok {
		t.Fatalf("no TracksRemovedFromLibrary emitted; got %v", rec.Names())
	}

	payload, ok := ev.Payload().(map[string]any)
	if !ok {
		t.Fatalf("payload is %T, want a map", ev.Payload())
	}

	got, ok := payload["filePaths"].([]string)
	if !ok || len(got) != 1 || got[0] != paths[0] {
		t.Errorf("payload filePaths = %v, want [%s]", payload["filePaths"], paths[0])
	}

	if count, ok := payload["count"].(int64); !ok || count != 1 {
		t.Errorf("payload count = %v, want 1", payload["count"])
	}
}

// TestRemoveFromLibrary_CompactsTheQueue pins the half that is invisible
// from the track list: deleting an audio_files row cascades to
// queue_tracks, so the queue's in-memory copy — and the player, if it
// was the track playing — has to be told.
func TestRemoveFromLibrary_CompactsTheQueue(t *testing.T) {
	t.Parallel()

	lib, dir, paths, _, libID := setupScanLibrary(t, 2)

	lib.scanInternal(libID, "Test", dir)

	compacted := 0

	lib.SetRemovalHooks(RemovalHooks{
		CompactQueue: func() { compacted++ },
	})

	if _, err := lib.RemoveFromLibrary([]string{paths[0]}); err != nil {
		t.Fatalf("RemoveFromLibrary: %v", err)
	}

	if compacted != 1 {
		t.Errorf("CompactQueue called %d times, want 1", compacted)
	}
}

// TestRemoveFromLibrary_RejectsAnEmptyRequest keeps a stray Delete on
// an empty selection from reaching the database at all.
func TestRemoveFromLibrary_RejectsAnEmptyRequest(t *testing.T) {
	t.Parallel()

	lib, _, _, _, _ := setupScanLibrary(t, 1)

	if _, err := lib.RemoveFromLibrary(nil); err == nil {
		t.Error("RemoveFromLibrary(nil) succeeded, want an error")
	}
}
