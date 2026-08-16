package library

import (
	"testing"

	"yellowjacket/backend/database"
	"yellowjacket/backend/database/sql/sqlcgen"
)

// seedAlbumsAndGenres builds two albums in two libraries, with one track
// carrying two genres — enough shape for the batched path lookups to be
// wrong in an interesting way if they group or filter incorrectly.
func seedAlbumsAndGenres(t *testing.T, lib *Library) (albumIDs []int64, libraryID int64) {
	t.Helper()

	ctx := lib.ctx
	q := lib.db.Queries

	library, err := q.CreateLibrary(ctx, sqlcgen.CreateLibraryParams{
		Name: "Main",
		Path: "/music",
	})
	if err != nil {
		t.Fatalf("create library: %v", err)
	}

	other, err := q.CreateLibrary(ctx, sqlcgen.CreateLibraryParams{
		Name: "Other",
		Path: "/other",
	})
	if err != nil {
		t.Fatalf("create other library: %v", err)
	}

	// Two albums; the second lives in the other library so the
	// library-scoped variants have something to exclude.
	type spec struct {
		album   string
		track   string
		path    string
		library int64
		disc    int64
		number  int64
		genres  []string
	}

	specs := []spec{
		{"First", "A2", "/music/a2.mp3", library.ID, 1, 2, []string{"Ambient"}},
		{"First", "A1", "/music/a1.mp3", library.ID, 1, 1, []string{"Ambient", "Baroque"}},
		{"Second", "B1", "/other/b1.mp3", other.ID, 1, 1, []string{"Baroque"}},
	}

	seen := map[string]bool{}

	for _, s := range specs {
		database.InsertTestTrack(t, lib.db, database.TestTrack{
			FilePath:    s.path,
			Title:       s.track,
			Artist:      "Test Artist",
			Album:       s.album,
			Genres:      s.genres,
			TrackNumber: s.number,
			DiscNumber:  s.disc,
			LibraryID:   s.library,
			LengthMs:    1000,
		})

		if !seen[s.album] {
			seen[s.album] = true

			var id int64
			if err := lib.db.QueryRowWriter(
				"SELECT id FROM albums WHERE name = ?", s.album,
			).Scan(&id); err != nil {
				t.Fatalf("album id for %q: %v", s.album, err)
			}

			albumIDs = append(albumIDs, id)
		}
	}

	return albumIDs, library.ID
}

// perf.m2: "play this artist" asked for whole track rows, one round trip
// per album, to read one field off each.  These two answer in one query,
// and the thing worth pinning is that they still group by the entity the
// caller ordered by — a flattened result would silently reorder a queue.
func TestGetFilePathsByAlbums(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)
	albumIDs, libraryID := seedAlbumsAndGenres(t, lib)

	got, err := lib.GetFilePathsByAlbums(albumIDs, 0)
	if err != nil {
		t.Fatalf("GetFilePathsByAlbums: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("albums returned = %d, want 2", len(got))
	}

	// Ordered by disc then track within an album, not by insertion.
	first := got[albumIDs[0]]
	if len(first) != 2 || first[0] != "/music/a1.mp3" || first[1] != "/music/a2.mp3" {
		t.Errorf("first album paths = %v, want [a1 a2] in track order", first)
	}

	scoped, err := lib.GetFilePathsByAlbums(albumIDs, libraryID)
	if err != nil {
		t.Fatalf("GetFilePathsByAlbums scoped: %v", err)
	}

	if _, ok := scoped[albumIDs[1]]; ok {
		t.Errorf("library-scoped result includes an album from another library")
	}

	if len(scoped[albumIDs[0]]) != 2 {
		t.Errorf("scoped first album = %v, want 2 paths", scoped[albumIDs[0]])
	}
}

func TestGetFilePathsByAlbums_Empty(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)

	got, err := lib.GetFilePathsByAlbums(nil, 0)
	if err != nil {
		t.Fatalf("GetFilePathsByAlbums(nil): %v", err)
	}

	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestGetFilePathsByGenres(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)
	_, libraryID := seedAlbumsAndGenres(t, lib)

	got, err := lib.GetFilePathsByGenres([]string{"Ambient", "Baroque"}, 0)
	if err != nil {
		t.Fatalf("GetFilePathsByGenres: %v", err)
	}

	if len(got["Ambient"]) != 2 {
		t.Errorf("Ambient = %v, want 2 paths", got["Ambient"])
	}

	// One track is in both genres: the overlap is returned under each,
	// because de-duplicating is the caller's job — it is the one that
	// knows the order the genres were selected in.
	if len(got["Baroque"]) != 2 {
		t.Errorf("Baroque = %v, want 2 paths", got["Baroque"])
	}

	scoped, err := lib.GetFilePathsByGenres([]string{"Baroque"}, libraryID)
	if err != nil {
		t.Fatalf("GetFilePathsByGenres scoped: %v", err)
	}

	if len(scoped["Baroque"]) != 1 || scoped["Baroque"][0] != "/music/a1.mp3" {
		t.Errorf("scoped Baroque = %v, want just the main library's track", scoped["Baroque"])
	}
}

func TestGetFilePathsByGenres_Empty(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)

	got, err := lib.GetFilePathsByGenres(nil, 0)
	if err != nil {
		t.Fatalf("GetFilePathsByGenres(nil): %v", err)
	}

	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

// seedRecordingMBIDs stamps recording MBIDs onto the tracks seeded by
// seedAlbumsAndGenres, in the shape the catalog side actually meets: two
// tracks tagged, one deliberately left untagged, and one MBID carried by
// two files in different libraries — which is what a duplicate is.
func seedRecordingMBIDs(t *testing.T, lib *Library) (tagged, shared string) {
	t.Helper()

	tagged = "11111111-1111-1111-1111-111111111111"
	shared = "22222222-2222-2222-2222-222222222222"

	byPath := map[string]string{
		"/music/a1.mp3": tagged,
		"/music/a2.mp3": shared,
		"/other/b1.mp3": shared,
	}

	for path, mbid := range byPath {
		if _, err := lib.db.ExecContext(
			"UPDATE audio_files SET recording_mbid = ? WHERE file_path = ?",
			mbid, path,
		); err != nil {
			t.Fatalf("set recording mbid for %s: %v", path, err)
		}
	}

	return tagged, shared
}

// The catalog side of the same finding: an Explore album page knows what
// the user owns only as recording MBIDs, so this is the lookup that
// turns "you own 7 of these 12" into something playable.
func TestGetFilePathsByRecordingMBIDs(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)
	_, libraryID := seedAlbumsAndGenres(t, lib)
	tagged, shared := seedRecordingMBIDs(t, lib)

	got, err := lib.GetFilePathsByRecordingMBIDs([]string{tagged, shared}, 0)
	if err != nil {
		t.Fatalf("GetFilePathsByRecordingMBIDs: %v", err)
	}

	if len(got[tagged]) != 1 || got[tagged][0] != "/music/a1.mp3" {
		t.Errorf("tagged recording = %v, want [/music/a1.mp3]", got[tagged])
	}

	// One recording, two files: grouping is what keeps that visible.
	// A flattened result could not say which was which.
	if len(got[shared]) != 2 {
		t.Errorf("shared recording = %v, want two paths", got[shared])
	}

	// Scoping drops the copy in the other library, and nothing else.
	scoped, err := lib.GetFilePathsByRecordingMBIDs([]string{tagged, shared}, libraryID)
	if err != nil {
		t.Fatalf("GetFilePathsByRecordingMBIDs scoped: %v", err)
	}

	if len(scoped[shared]) != 1 || scoped[shared][0] != "/music/a2.mp3" {
		t.Errorf("scoped shared = %v, want [/music/a2.mp3]", scoped[shared])
	}
}

// An empty MBID matches every untagged recording in the library, which
// is the opposite of the question being asked — so an unknown track must
// contribute nothing rather than everything.
func TestGetFilePathsByRecordingMBIDs_IgnoresEmpty(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)
	seedAlbumsAndGenres(t, lib)
	seedRecordingMBIDs(t, lib)

	got, err := lib.GetFilePathsByRecordingMBIDs([]string{"", ""}, 0)
	if err != nil {
		t.Fatalf("GetFilePathsByRecordingMBIDs: %v", err)
	}

	if len(got) != 0 {
		t.Errorf("empty MBIDs matched %d recordings, want none", len(got))
	}

	if _, err := lib.GetFilePathsByRecordingMBIDs(nil, 0); err != nil {
		t.Fatalf("nil MBIDs: %v", err)
	}
}
