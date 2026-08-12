package library

import (
	"database/sql"
	"testing"

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

	ac, err := q.UpsertArtistCredit(ctx, "Test Artist")
	if err != nil {
		t.Fatalf("upsert artist credit: %v", err)
	}

	genreIDs := map[string]int64{}

	for _, name := range []string{"Ambient", "Baroque"} {
		g, err := q.UpsertGenre(ctx, name)
		if err != nil {
			t.Fatalf("upsert genre %s: %v", name, err)
		}

		genreIDs[name] = g.ID
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

	byAlbum := map[string]int64{}

	for _, s := range specs {
		rec, err := q.CreateRecordingFull(ctx, sqlcgen.CreateRecordingFullParams{
			Name:           s.track,
			ArtistCreditID: ac.ID,
		})
		if err != nil {
			t.Fatalf("create recording: %v", err)
		}

		rgID, ok := byAlbum[s.album]

		if !ok {
			rg, err := q.UpsertReleaseGroup(ctx, sqlcgen.UpsertReleaseGroupParams{
				Name:                s.album,
				AlbumArtistCreditID: sql.NullInt64{Int64: ac.ID, Valid: true},
			})
			if err != nil {
				t.Fatalf("upsert release group: %v", err)
			}

			rgID = rg.ID
			byAlbum[s.album] = rgID
			albumIDs = append(albumIDs, rgID)
		}

		if _, err := q.CreateReleaseGroupRecording(
			ctx, sqlcgen.CreateReleaseGroupRecordingParams{
				ReleaseGroupID: rgID,
				RecordingID:    rec.ID,
				TrackNumber:    sql.NullInt64{Int64: s.number, Valid: true},
				DiscNumber:     sql.NullInt64{Int64: s.disc, Valid: true},
			},
		); err != nil {
			t.Fatalf("link recording: %v", err)
		}

		if _, err := q.CreateAudioFile(ctx, sqlcgen.CreateAudioFileParams{
			FilePath:           s.path,
			LengthMilliseconds: 1000,
			RecordingID:        rec.ID,
			LibraryID:          s.library,
			Basename:           s.track + ".mp3",
		}); err != nil {
			t.Fatalf("create audio file: %v", err)
		}

		for _, g := range s.genres {
			if err := q.CreateRecordingGenre(
				ctx, sqlcgen.CreateRecordingGenreParams{
					RecordingID: rec.ID,
					GenreID:     genreIDs[g],
				},
			); err != nil {
				t.Fatalf("link genre: %v", err)
			}
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
