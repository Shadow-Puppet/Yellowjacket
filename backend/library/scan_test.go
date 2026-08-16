package library

import (
	"database/sql"
	"log/slog"
	"sync/atomic"
	"testing"

	"yellowjacket/backend/database"
	"yellowjacket/backend/database/sql/sqlcgen"
	"yellowjacket/backend/metadata"
)

// ---------------------------------------------------------------------------
// Pure helper tests — no database dependency
// ---------------------------------------------------------------------------

func TestGetRecordingName(t *testing.T) {
	t.Parallel()

	lib := &Library{} // getRecordingName uses only tags + filePath

	tests := []struct {
		name     string
		title    string
		filePath string
		want     string
	}{
		{
			name:     "title present",
			title:    "Bohemian Rhapsody",
			filePath: "/music/queen/bohemian.mp3",
			want:     "Bohemian Rhapsody",
		},
		{
			name:     "title empty falls back to filename sans extension",
			title:    "",
			filePath: "/music/song.mp3",
			want:     "song",
		},
		{
			name:     "title empty with complex filename",
			title:    "",
			filePath: "/music/Artist - Track.flac",
			want:     "Artist - Track",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tags := &metadata.TrackMetadata{Title: tt.title}
			got := lib.getRecordingName(tags, tt.filePath)

			if got != tt.want {
				t.Errorf("getRecordingName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToNullInt64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input int
		want  sql.NullInt64
	}{
		{
			name:  "zero is null",
			input: 0,
			want:  sql.NullInt64{},
		},
		{
			name:  "positive is valid",
			input: 5,
			want:  sql.NullInt64{Int64: 5, Valid: true},
		},
		{
			name:  "negative is valid",
			input: -1,
			want:  sql.NullInt64{Int64: -1, Valid: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := toNullInt64(tt.input)
			if got != tt.want {
				t.Errorf("toNullInt64(%d) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestToNullString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  sql.NullString
	}{
		{
			name:  "empty is null",
			input: "",
			want:  sql.NullString{},
		},
		{
			name:  "non-empty is valid",
			input: "rock",
			want:  sql.NullString{String: "rock", Valid: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := toNullString(tt.input)
			if got != tt.want {
				t.Errorf("toNullString(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSplitGenres(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "empty string returns nil",
			input: "",
			want:  nil,
		},
		{
			name:  "single genre",
			input: "Rock",
			want:  []string{"Rock"},
		},
		{
			name:  "multiple genres",
			input: "Rock||Jazz||Blues",
			want:  []string{"Rock", "Jazz", "Blues"},
		},
		{
			name:  "two genres",
			input: "Electronic||Ambient",
			want:  []string{"Electronic", "Ambient"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := splitGenres(tt.input)

			if tt.want == nil {
				if got != nil {
					t.Errorf("splitGenres(%q) = %v, want nil", tt.input, got)
				}

				return
			}

			if len(got) != len(tt.want) {
				t.Fatalf("splitGenres(%q) length = %d, want %d", tt.input, len(got), len(tt.want))
			}

			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("splitGenres(%q)[%d] = %q, want %q", tt.input, i, v, tt.want[i])
				}
			}
		})
	}
}

func TestTrackFromRow(t *testing.T) {
	t.Parallel()

	track := trackFromRow(sqlcgen.TrackMetadatum{
		FilePath:           "/music/queen/bohemian.flac",
		LengthMilliseconds: 180000,
		Title:              "Bohemian Rhapsody",
		ArtistName:         "Queen",
		TrackNumber:        sql.NullInt64{Int64: 1, Valid: true},
		DiscNumber:         sql.NullInt64{Int64: 1, Valid: true},
		Album:              "A Night at the Opera",
		Genre:              "Rock||Progressive Rock",
		Year:               1975,
		Composer:           "Freddie Mercury",
		FileType:           ".flac",
		SampleRate:         44100,
		BitDepth:           16,
		Channels:           2,
		Bitrate:            1411,
		FileSize:           35000000,
	})

	if track.TrackName != "Bohemian Rhapsody" {
		t.Errorf("TrackName = %q, want %q", track.TrackName, "Bohemian Rhapsody")
	}

	if track.ArtistName != "Queen" {
		t.Errorf("ArtistName = %q, want %q", track.ArtistName, "Queen")
	}

	if track.TrackLength != "180000" {
		t.Errorf("TrackLength = %q, want %q", track.TrackLength, "180000")
	}

	if len(track.Genre) != 2 || track.Genre[0] != "Rock" ||
		track.Genre[1] != "Progressive Rock" {
		t.Errorf("Genre = %v, want [Rock, Progressive Rock]", track.Genre)
	}

	if track.Year != 1975 {
		t.Errorf("Year = %d, want %d", track.Year, 1975)
	}

	if track.SampleRate != 44100 {
		t.Errorf("SampleRate = %d, want %d", track.SampleRate, 44100)
	}

	if track.BitDepth != 16 {
		t.Errorf("BitDepth = %d, want %d", track.BitDepth, 16)
	}

	if track.Channels != 2 {
		t.Errorf("Channels = %d, want %d", track.Channels, 2)
	}

	if track.Bitrate != 1411 {
		t.Errorf("Bitrate = %d, want %d", track.Bitrate, 1411)
	}

	if track.FileSize != 35000000 {
		t.Errorf("FileSize = %d, want %d", track.FileSize, 35000000)
	}

	// A NULL track/disc number yields 0, not a panic.
	trackNull := trackFromRow(sqlcgen.TrackMetadatum{
		FilePath:   "/music/unknown.mp3",
		Title:      "Test",
		ArtistName: "Artist",
	})

	if trackNull.TrackNumber != 0 {
		t.Errorf("null TrackNumber = %d, want 0", trackNull.TrackNumber)
	}

	if trackNull.DiscNumber != 0 {
		t.Errorf("null DiscNumber = %d, want 0", trackNull.DiscNumber)
	}

	if trackNull.Genre != nil {
		t.Errorf("empty Genre = %v, want nil", trackNull.Genre)
	}
}

func setupTestLibrary(t *testing.T) (*Library, *database.DB) {
	t.Helper()

	db := database.NewTestDB(t)

	// Construct Library directly (internal test) — avoids Config.Validate
	// calling os.Stat on the directory.  Entity cache functions only need
	// l.ctx and l.db; they have no Wails runtime dependency.
	lib := &Library{
		ctx:    t.Context(),
		logger: slog.Default(),
		conf:   &Config{},
		db:     db,
	}

	return lib, db
}

// ---------------------------------------------------------------------------
// Entity cache tests — DB-backed
// ---------------------------------------------------------------------------

func TestCachedUpsertArtist(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)
	cache := newEntityCache()
	q := lib.db.Queries

	first := lib.cachedUpsertArtist(q, cache, "Queen", "")
	if first.ID == 0 {
		t.Fatal("expected non-zero artist ID")
	}

	// Second call is a cache hit and returns the same row.
	if second := lib.cachedUpsertArtist(q, cache, "Queen", ""); second.ID != first.ID {
		t.Errorf("cache miss: got ID %d, want %d", second.ID, first.ID)
	}

	if other := lib.cachedUpsertArtist(q, cache, "Beyonce", ""); other.ID == first.ID {
		t.Errorf("different name returned same ID %d", other.ID)
	}

	if len(cache.artists) != 2 {
		t.Errorf("cache entries = %d, want 2", len(cache.artists))
	}

	// An MBID arriving on a later file is written to the cached row -
	// the first file of an album often has no MBID and a later one does.
	withMBID := lib.cachedUpsertArtist(q, cache, "Queen", "mbid-queen")
	if !withMBID.Mbid.Valid || withMBID.Mbid.String != "mbid-queen" {
		t.Errorf("artist mbid = %v, want mbid-queen", withMBID.Mbid)
	}

	// An empty name is not a missing row: it becomes "Unknown Artist",
	// because a file with no artist tag still has to belong somewhere.
	unknown := lib.cachedUpsertArtist(q, cache, "", "")
	if unknown.Name != "Unknown Artist" {
		t.Errorf("empty artist name = %q, want %q", unknown.Name, "Unknown Artist")
	}
}

func TestCachedUpsertAlbum(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)
	cache := newEntityCache()
	q := lib.db.Queries

	first := lib.cachedUpsertAlbum(q, cache, albumParams{
		name:   "A Night at the Opera",
		credit: "Queen",
	})
	if first.ID == 0 {
		t.Fatal("expected non-zero album ID")
	}

	same := lib.cachedUpsertAlbum(q, cache, albumParams{
		name:   "A Night at the Opera",
		credit: "Queen",
	})
	if same.ID != first.ID {
		t.Errorf("cache miss: got ID %d, want %d", same.ID, first.ID)
	}

	// Album identity is (name, credit), so the same title by someone
	// else is a different album.
	other := lib.cachedUpsertAlbum(q, cache, albumParams{
		name:   "A Night at the Opera",
		credit: "Blind Guardian",
	})
	if other.ID == first.ID {
		t.Error("same album name by a different artist collapsed into one album")
	}
}

func TestCachedUpsertAlbum_FillsCoverArtLater(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)
	cache := newEntityCache()
	q := lib.db.Queries

	album := lib.cachedUpsertAlbum(q, cache, albumParams{name: "Art", credit: "A"})

	ca, err := q.UpsertCoverArt(lib.ctx, sqlcgen.UpsertCoverArtParams{
		FilePath: "/covers/art.jpg",
		MimeType: "image/jpeg",
	})
	if err != nil {
		t.Fatalf("upsert cover art: %v", err)
	}

	// The first file of an album often carries no embedded art and a
	// later one does; the album has to pick it up.
	withArt := lib.cachedUpsertAlbum(q, cache, albumParams{
		name:       "Art",
		credit:     "A",
		coverArtID: sql.NullInt64{Int64: ca.ID, Valid: true},
	})

	if withArt.ID != album.ID {
		t.Fatalf("album ID changed: got %d, want %d", withArt.ID, album.ID)
	}

	if !withArt.CoverArtID.Valid || withArt.CoverArtID.Int64 != ca.ID {
		t.Errorf("cover art = %v, want %d", withArt.CoverArtID, ca.ID)
	}
}

func TestCachedUpsertGenre(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)
	cache := newEntityCache()
	q := lib.db.Queries

	first, err := lib.cachedUpsertGenre(q, cache, "Rock")
	if err != nil {
		t.Fatalf("cachedUpsertGenre: %v", err)
	}

	second, err := lib.cachedUpsertGenre(q, cache, "Rock")
	if err != nil {
		t.Fatalf("cachedUpsertGenre (cached): %v", err)
	}

	if second.ID != first.ID {
		t.Errorf("cache miss: got ID %d, want %d", second.ID, first.ID)
	}
}

// TestPruneEmptyEntities is what is left of four orphan-sweep tests.
//
// Three of the tables they covered are gone, and with them the bug they
// were guarding: a file used to create a recording, a credit, a
// credit-artist link and a release-group link, none of which were
// deleted when the file was, so a real library accumulated 812
// recordings, 216 release groups and 260 artists with nothing behind
// them.  Two tables can still be left empty by a removal, and this is
// that.
func TestPruneEmptyEntities(t *testing.T) {
	t.Parallel()

	lib, db := setupTestLibrary(t)

	kept := database.InsertTestTrack(t, db, database.TestTrack{
		FilePath: "/music/kept.mp3",
		Title:    "Kept",
		Artist:   "Kept Artist",
		Album:    "Kept Album",
		Genres:   []string{"Kept Genre"},
	})
	gone := database.InsertTestTrack(t, db, database.TestTrack{
		FilePath: "/music/gone.mp3",
		Title:    "Gone",
		Artist:   "Gone Artist",
		Album:    "Gone Album",
		Genres:   []string{"Gone Genre"},
	})

	_ = kept

	if err := db.Queries.DeleteAudioFile(lib.ctx, gone); err != nil {
		t.Fatalf("delete audio file: %v", err)
	}

	lib.pruneEmptyEntities()

	for _, c := range []struct {
		table string
		name  string
		want  int
	}{
		{"albums", "Gone Album", 0},
		{"albums", "Kept Album", 1},
		{"artists", "Gone Artist", 0},
		{"artists", "Kept Artist", 1},
		{"genres", "Gone Genre", 0},
		{"genres", "Kept Genre", 1},
	} {
		var n int
		if err := db.QueryRowWriter(
			"SELECT COUNT(*) FROM "+c.table+" WHERE name = ?", c.name,
		).Scan(&n); err != nil {
			t.Fatalf("count %s %q: %v", c.table, c.name, err)
		}

		if n != c.want {
			t.Errorf("%s %q rows = %d, want %d", c.table, c.name, n, c.want)
		}
	}
}

func TestCommitBatch_TaggingItemsBookkeeping(t *testing.T) {
	t.Parallel()

	lib, db := setupTestLibrary(t)
	cache := newEntityCache()
	metrics := newScanMetrics()

	var added, updated, skipped atomic.Int64

	batch := []importResult{
		{
			absolutePath: "/music/Artist/Album 1/01.mp3",
			fileType:     metadata.MP3,
			lengthMillis: 200000,
			tags: &metadata.TrackMetadata{
				Title: "A1T1", Artist: "Artist", AlbumArtist: "Artist",
				Album: "Album 1", TrackNumber: 1, DiscNumber: 0,
			},
			libraryID: 0,
		},
		{
			absolutePath: "/music/Artist/Album 1/02.mp3",
			fileType:     metadata.MP3,
			lengthMillis: 210000,
			tags: &metadata.TrackMetadata{
				Title: "A1T2", Artist: "Artist", AlbumArtist: "Artist",
				Album: "Album 1", TrackNumber: 2, DiscNumber: 0,
			},
			libraryID: 0,
		},
		{
			absolutePath: "/music/Artist/Album 2 [Disc 1]/01.mp3",
			fileType:     metadata.MP3,
			lengthMillis: 220000,
			tags: &metadata.TrackMetadata{
				Title: "A2D1T1", Artist: "Artist", AlbumArtist: "Artist",
				Album: "Album 2", TrackNumber: 1, DiscNumber: 1,
			},
			libraryID: 0,
		},
		{
			absolutePath: "/music/Artist/Album 2 [Disc 2]/01.mp3",
			fileType:     metadata.MP3,
			lengthMillis: 230000,
			tags: &metadata.TrackMetadata{
				Title: "A2D2T1", Artist: "Artist", AlbumArtist: "Artist",
				Album: "Album 2", TrackNumber: 1, DiscNumber: 2,
			},
			libraryID: 0,
		},
		{
			absolutePath: "/music/Orphan/singleton.mp3",
			fileType:     metadata.MP3,
			lengthMillis: 100000,
			tags: &metadata.TrackMetadata{
				Title: "Orphan", Artist: "Solo", AlbumArtist: "Solo",
				Album: "", TrackNumber: 0, DiscNumber: 0,
			},
			libraryID: 0,
		},
	}

	if err := lib.commitBatch(batch, cache, metrics, &added, &updated, &skipped, nil); err != nil {
		t.Fatalf("commitBatch: %v", err)
	}

	if added.Load() != int64(len(batch)) {
		for _, w := range metrics.Warnings {
			t.Logf("warning: path=%s phase=%s err=%s", w.FilePath, w.Phase, w.Err)
		}

		t.Fatalf("added = %d, want %d (skipped=%d)", added.Load(), len(batch), skipped.Load())
	}

	groupCount := queryInt(t, db, "SELECT COUNT(*) FROM tagging_items")

	if groupCount != 4 {
		t.Errorf("tagging_items count = %d, want 4", groupCount)
	}

	// Each group should carry the expected track_count.
	wantCounts := map[[3]any]int64{
		{int64(0), "Album 1", int64(0)}: 2,
		{int64(0), "Album 2", int64(1)}: 1,
		{int64(0), "Album 2", int64(2)}: 1,
		{int64(0), "", int64(0)}:        1,
	}

	for key, want := range wantCounts {
		libID, _ := key[0].(int64)
		album, _ := key[1].(string)
		disc, _ := key[2].(int64)

		rows, err := db.QueryContext(
			`SELECT track_count FROM tagging_items
			 WHERE library_id = ? AND album_name = ? AND disc_number = ?`,
			libID, album, disc,
		)
		if err != nil {
			t.Errorf("query track_count for %v: %v", key, err)

			continue
		}

		var got int64
		if rows.Next() {
			if scanErr := rows.Scan(&got); scanErr != nil {
				t.Errorf("scan track_count for %v: %v", key, scanErr)
			}
		}

		_ = rows.Close()

		if got != want {
			t.Errorf("track_count for %v = %d, want %d", key, got, want)
		}
	}
}

func TestCommitBatch_AlbumTagChangeKeepsGroup(t *testing.T) {
	t.Parallel()

	// Folder-based grouping: if a track stays in the same folder
	// but its album tag changes (very common — autotag itself
	// rewrites album tags), the group_key should NOT change.  Test
	// guards against the old behavior where any album-tag drift
	// would split the album into multiple groups.
	lib, db := setupTestLibrary(t)
	cache := newEntityCache()
	metrics := newScanMetrics()

	var added, updated, skipped atomic.Int64

	initial := []importResult{
		{
			absolutePath: "/music/Artist/Album Folder/01.mp3",
			fileType:     metadata.MP3,
			lengthMillis: 200000,
			tags: &metadata.TrackMetadata{
				Title: "Track", Artist: "Artist", AlbumArtist: "Artist",
				Album: "Old Album",
			},
			libraryID: 0,
		},
	}

	if err := lib.commitBatch(
		initial,
		cache,
		metrics,
		&added,
		&updated,
		&skipped,
		nil,
	); err != nil {
		t.Fatalf("initial commitBatch: %v", err)
	}

	var (
		fileID        int64
		originalGroup string
	)

	rows, err := db.QueryContext(
		`SELECT id, group_key FROM audio_files WHERE file_path = ?`,
		"/music/Artist/Album Folder/01.mp3",
	)
	if err != nil {
		t.Fatalf("lookup file: %v", err)
	}

	if rows.Next() {
		if scanErr := rows.Scan(&fileID, &originalGroup); scanErr != nil {
			t.Fatalf("scan file: %v", scanErr)
		}
	}

	_ = rows.Close()

	update := []importResult{
		{
			absolutePath:   "/music/Artist/Album Folder/01.mp3",
			fileType:       metadata.MP3,
			lengthMillis:   200000,
			existingFileID: fileID,
			needsUpdate:    true,
			tags: &metadata.TrackMetadata{
				Title: "Track", Artist: "Artist", AlbumArtist: "Artist",
				Album: "Albums Canonical Name (Remastered 2024)",
			},
			libraryID: 0,
		},
	}

	if err := lib.commitBatch(update, cache, metrics, &added, &updated, &skipped, nil); err != nil {
		t.Fatalf("update commitBatch: %v", err)
	}

	groupCount := queryInt(t, db, `SELECT COUNT(*) FROM tagging_items`)
	if groupCount != 1 {
		t.Errorf("expected exactly 1 tagging_items row, got %d", groupCount)
	}

	rows2, err := db.QueryContext(
		`SELECT group_key FROM audio_files WHERE id = ?`, fileID,
	)
	if err != nil {
		t.Fatalf("lookup post-update: %v", err)
	}

	var afterGroup string
	if rows2.Next() {
		if scanErr := rows2.Scan(&afterGroup); scanErr != nil {
			t.Fatalf("scan post-update: %v", scanErr)
		}
	}

	_ = rows2.Close()

	if afterGroup != originalGroup {
		t.Errorf("group_key changed across album tag edit: %q → %q", originalGroup, afterGroup)
	}
}

func TestCommitBatch_RescanPromotesTagStatus(t *testing.T) {
	t.Parallel()

	// Only the insert path stamps tag_status, so a file another
	// tagger stamped with MBIDs after import used to keep 'untagged'
	// for ever — and its folder kept asking to be tagged, since that
	// column is what the autotag queue reads.
	lib, db := setupTestLibrary(t)
	cache := newEntityCache()
	metrics := newScanMetrics()

	var added, updated, skipped atomic.Int64

	const path = "/music/Artist/Album Folder/01.mp3"

	initial := []importResult{
		{
			absolutePath: path,
			fileType:     metadata.MP3,
			lengthMillis: 200000,
			tags: &metadata.TrackMetadata{
				Title: "Track", Artist: "Artist", AlbumArtist: "Artist",
				Album: "Album",
			},
			libraryID: 0,
		},
	}

	if err := lib.commitBatch(
		initial, cache, metrics, &added, &updated, &skipped, nil,
	); err != nil {
		t.Fatalf("initial commitBatch: %v", err)
	}

	fileID := queryInt(t, db,
		`SELECT id FROM audio_files WHERE file_path = ?`, path,
	)

	if got := queryString(t, db,
		`SELECT tag_status FROM audio_files WHERE id = ?`, fileID,
	); got != "untagged" {
		t.Fatalf("tag_status after import = %q, want %q", got, "untagged")
	}

	update := []importResult{
		{
			absolutePath:   path,
			fileType:       metadata.MP3,
			lengthMillis:   200000,
			existingFileID: fileID,
			needsUpdate:    true,
			tags: &metadata.TrackMetadata{
				Title: "Track", Artist: "Artist", AlbumArtist: "Artist",
				Album:         "Album",
				RecordingMBID: "11111111-2222-3333-4444-555555555555",
			},
			libraryID: 0,
		},
	}

	if err := lib.commitBatch(
		update, cache, metrics, &added, &updated, &skipped, nil,
	); err != nil {
		t.Fatalf("update commitBatch: %v", err)
	}

	if got := queryString(t, db,
		`SELECT tag_status FROM audio_files WHERE id = ?`, fileID,
	); got != "user_confirmed" {
		t.Errorf("tag_status after rescan = %q, want %q", got, "user_confirmed")
	}

	// A deliberate "never ask me about this file again" outranks the
	// promotion: the guard is on 'untagged', not on the MBID.
	if _, err := db.ExecContext(
		`UPDATE audio_files SET tag_status = 'user_skipped_permanent' WHERE id = ?`,
		fileID,
	); err != nil {
		t.Fatalf("mark skipped: %v", err)
	}

	if err := lib.commitBatch(
		update, cache, metrics, &added, &updated, &skipped, nil,
	); err != nil {
		t.Fatalf("second update commitBatch: %v", err)
	}

	if got := queryString(t, db,
		`SELECT tag_status FROM audio_files WHERE id = ?`, fileID,
	); got != "user_skipped_permanent" {
		t.Errorf("tag_status = %q, want the skip to survive a rescan", got)
	}
}

// queryString is queryInt's text counterpart.
func queryString(t *testing.T, db *database.DB, query string, args ...any) string {
	t.Helper()

	rows, err := db.QueryContext(query, args...)
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}

	defer func() { _ = rows.Close() }()

	var out string

	if rows.Next() {
		if scanErr := rows.Scan(&out); scanErr != nil {
			t.Fatalf("scan: %v", scanErr)
		}
	}

	return out
}

// queryInt runs a single-column scalar query and returns the first
// int64 result; fails the test on any error.
func queryInt(t *testing.T, db *database.DB, query string, args ...any) int64 {
	t.Helper()

	rows, err := db.QueryContext(query, args...)
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}

	defer func() { _ = rows.Close() }()

	var got int64
	if rows.Next() {
		if err := rows.Scan(&got); err != nil {
			t.Fatalf("scan %q: %v", query, err)
		}
	}

	return got
}
