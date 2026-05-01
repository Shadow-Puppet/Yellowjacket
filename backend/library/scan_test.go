package library

import (
	"context"
	"database/sql"
	"fmt"
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

func TestMapTrackRow(t *testing.T) {
	t.Parallel()

	track := mapTrackRow(
		"/music/queen/bohemian.flac",         // filePath
		180000,                               // lengthMs
		"Bohemian Rhapsody",                  // title
		"Queen",                              // artistName
		sql.NullInt64{Int64: 1, Valid: true}, // trackNumber
		sql.NullInt64{Int64: 1, Valid: true}, // discNumber
		"A Night at the Opera",               // album
		"Rock||Progressive Rock",             // genre
		1975,                                 // year
		"Freddie Mercury",                    // composer
		".flac",                              // fileType
		44100,                                // sampleRate
		16,                                   // bitDepth
		2,                                    // channels
		1411,                                 // bitrate
		35000000,                             // fileSize
		0,                                    // playCount
		sql.NullTime{},                       // lastPlayed
		"",                                   // coverArtPath
		"",                                   // artistMBID
		"",                                   // releaseGroupMBID
		"",                                   // recordingMBID
	)

	// Verify all 16 fields.
	if track.TrackName != "Bohemian Rhapsody" {
		t.Errorf("TrackName = %q, want %q", track.TrackName, "Bohemian Rhapsody")
	}

	if track.ArtistName != "Queen" {
		t.Errorf("ArtistName = %q, want %q", track.ArtistName, "Queen")
	}

	// TrackLength is string-formatted milliseconds.
	if track.TrackLength != "180000" {
		t.Errorf("TrackLength = %q, want %q", track.TrackLength, "180000")
	}

	if track.FilePath != "/music/queen/bohemian.flac" {
		t.Errorf("FilePath = %q, want %q", track.FilePath, "/music/queen/bohemian.flac")
	}

	if track.TrackNumber != 1 {
		t.Errorf("TrackNumber = %d, want %d", track.TrackNumber, 1)
	}

	if track.DiscNumber != 1 {
		t.Errorf("DiscNumber = %d, want %d", track.DiscNumber, 1)
	}

	if track.Album != "A Night at the Opera" {
		t.Errorf("Album = %q, want %q", track.Album, "A Night at the Opera")
	}

	wantGenres := []string{"Rock", "Progressive Rock"}
	if len(track.Genre) != len(wantGenres) {
		t.Fatalf("Genre length = %d, want %d", len(track.Genre), len(wantGenres))
	}

	for i, g := range track.Genre {
		if g != wantGenres[i] {
			t.Errorf("Genre[%d] = %q, want %q", i, g, wantGenres[i])
		}
	}

	if track.Year != 1975 {
		t.Errorf("Year = %d, want %d", track.Year, 1975)
	}

	if track.Composer != "Freddie Mercury" {
		t.Errorf("Composer = %q, want %q", track.Composer, "Freddie Mercury")
	}

	if track.FileType != ".flac" {
		t.Errorf("FileType = %q, want %q", track.FileType, ".flac")
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

	// Verify NullInt64 with Valid=false yields 0.
	trackNull := mapTrackRow(
		"/music/unknown.mp3", 0, "Test", "Artist",
		sql.NullInt64{}, sql.NullInt64{}, // invalid (null)
		"", "", 0, "", "", 0, 0, 0, 0, 0,
		0,              // playCount
		sql.NullTime{}, // lastPlayed
		"",             // coverArtPath
		"", "", "",     // artistMBID, releaseGroupMBID, recordingMBID
	)

	if trackNull.TrackNumber != 0 {
		t.Errorf("null TrackNumber = %d, want 0", trackNull.TrackNumber)
	}

	if trackNull.DiscNumber != 0 {
		t.Errorf("null DiscNumber = %d, want 0", trackNull.DiscNumber)
	}
}

// ---------------------------------------------------------------------------
// Test helper — constructs a Library backed by an in-memory test DB
// ---------------------------------------------------------------------------

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

func TestCachedUpsertArtistCredit(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)
	cache := newEntityCache()
	q := lib.db.Queries

	// First call — hits DB.
	ac1, err := lib.cachedUpsertArtistCredit(q, cache, "Queen")
	if err != nil {
		t.Fatalf("first cachedUpsertArtistCredit: %v", err)
	}

	if ac1.ID == 0 {
		t.Fatal("expected non-zero ArtistCredit ID")
	}

	// Second call — cache hit, same ID.
	ac2, err := lib.cachedUpsertArtistCredit(q, cache, "Queen")
	if err != nil {
		t.Fatalf("second cachedUpsertArtistCredit: %v", err)
	}

	if ac2.ID != ac1.ID {
		t.Errorf("cache miss: got ID %d, want %d", ac2.ID, ac1.ID)
	}

	// Different name — different ID.
	ac3, err := lib.cachedUpsertArtistCredit(q, cache, "Beyoncé")
	if err != nil {
		t.Fatalf("cachedUpsertArtistCredit(Beyoncé): %v", err)
	}

	if ac3.ID == ac1.ID {
		t.Errorf("different name returned same ID %d", ac3.ID)
	}

	// Cache should have 2 entries.
	if len(cache.artistCredits) != 2 {
		t.Errorf("cache entries = %d, want 2", len(cache.artistCredits))
	}
}

func TestCachedLinkArtist(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)
	cache := newEntityCache()
	q := lib.db.Queries
	metrics := newScanMetrics()

	// Create an artist credit first.
	ac, err := lib.cachedUpsertArtistCredit(q, cache, "Queen")
	if err != nil {
		t.Fatalf("upsert artist credit: %v", err)
	}

	// First link — creates artist + artist-credit-artist link.
	lib.cachedLinkArtist(q, cache, metrics, "Queen", ac.ID)

	if len(cache.artists) != 1 {
		t.Errorf("artists cache = %d, want 1", len(cache.artists))
	}

	if len(cache.linkedCredits) != 1 {
		t.Errorf("linkedCredits cache = %d, want 1", len(cache.linkedCredits))
	}

	// Second call with same args — should skip (cache hit).
	lib.cachedLinkArtist(q, cache, metrics, "Queen", ac.ID)

	if len(cache.linkedCredits) != 1 {
		t.Errorf(
			"linkedCredits after duplicate = %d, want 1 (should skip)",
			len(cache.linkedCredits),
		)
	}
}

func TestCachedLinkArtist_MultiCredit(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)
	cache := newEntityCache()
	q := lib.db.Queries
	metrics := newScanMetrics()

	// Two different artist credits referencing the same artist name.
	ac1, err := lib.cachedUpsertArtistCredit(q, cache, "Queen")
	if err != nil {
		t.Fatalf("upsert credit 1: %v", err)
	}

	ac2, err := lib.cachedUpsertArtistCredit(q, cache, "Queen feat. David Bowie")
	if err != nil {
		t.Fatalf("upsert credit 2: %v", err)
	}

	// Link "Queen" artist to both credits.
	lib.cachedLinkArtist(q, cache, metrics, "Queen", ac1.ID)
	lib.cachedLinkArtist(q, cache, metrics, "Queen", ac2.ID)

	// Artist cached once.
	if len(cache.artists) != 1 {
		t.Errorf("artists cache = %d, want 1 (same artist name)", len(cache.artists))
	}

	// Two distinct linked-credit entries.
	if len(cache.linkedCredits) != 2 {
		t.Errorf("linkedCredits = %d, want 2", len(cache.linkedCredits))
	}

	// Verify link keys are correct format.
	queenArtist := cache.artists["Queen"]
	key1 := fmt.Sprintf("%d:%d", queenArtist.ID, ac1.ID)
	key2 := fmt.Sprintf("%d:%d", queenArtist.ID, ac2.ID)

	if _, ok := cache.linkedCredits[key1]; !ok {
		t.Errorf("missing linked credit key %q", key1)
	}

	if _, ok := cache.linkedCredits[key2]; !ok {
		t.Errorf("missing linked credit key %q", key2)
	}
}

func TestCachedUpsertGenre(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)
	cache := newEntityCache()
	q := lib.db.Queries

	// First call — creates genre.
	g1, err := lib.cachedUpsertGenre(q, cache, "Rock")
	if err != nil {
		t.Fatalf("first cachedUpsertGenre: %v", err)
	}

	if g1.ID == 0 {
		t.Fatal("expected non-zero Genre ID")
	}

	// Second call — cache hit.
	g2, err := lib.cachedUpsertGenre(q, cache, "Rock")
	if err != nil {
		t.Fatalf("second cachedUpsertGenre: %v", err)
	}

	if g2.ID != g1.ID {
		t.Errorf("cache miss: got ID %d, want %d", g2.ID, g1.ID)
	}

	if len(cache.genres) != 1 {
		t.Errorf("genre cache entries = %d, want 1", len(cache.genres))
	}
}

func TestResolveReleaseGroup(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)
	cache := newEntityCache()
	q := lib.db.Queries

	// Need an album artist credit for the release group.
	ac, err := lib.cachedUpsertArtistCredit(q, cache, "Queen")
	if err != nil {
		t.Fatalf("upsert artist credit: %v", err)
	}

	albumArtistCreditID := sql.NullInt64{Int64: ac.ID, Valid: true}

	// First call — no cover art.
	tags := &metadata.TrackMetadata{
		Album: "A Night at the Opera",
		Year:  1975,
	}

	rgID := lib.resolveReleaseGroup(q, cache, tags, albumArtistCreditID, sql.NullInt64{})
	if !rgID.Valid {
		t.Fatal("expected valid release group ID")
	}

	if rgID.Int64 == 0 {
		t.Fatal("expected non-zero release group ID")
	}

	// Verify cached.
	if len(cache.releaseGroups) != 1 {
		t.Errorf("releaseGroups cache = %d, want 1", len(cache.releaseGroups))
	}

	// Second call — same album with cover art → should update cover art on cached entry.
	// First, create a cover art record in the DB.
	coverArt, err := q.UpsertCoverArt(lib.ctx, sqlcgen.UpsertCoverArtParams{
		IsEmbedded: true,
		FilePath:   "/covers/opera.jpg",
		MimeType:   "image/jpeg",
	})
	if err != nil {
		t.Fatalf("create cover art: %v", err)
	}

	coverArtID := sql.NullInt64{Int64: coverArt.ID, Valid: true}
	rgID2 := lib.resolveReleaseGroup(q, cache, tags, albumArtistCreditID, coverArtID)

	if rgID2.Int64 != rgID.Int64 {
		t.Errorf("cache miss: got ID %d, want %d", rgID2.Int64, rgID.Int64)
	}

	// Cover art should be updated on the cached release group.
	// Cache key is composite: "albumName\x00artistCreditID".
	cacheKey := fmt.Sprintf("%s\x00%d", "A Night at the Opera", ac.ID)
	cachedRG := cache.releaseGroups[cacheKey]

	if !cachedRG.CoverArtID.Valid {
		t.Error("expected CoverArtID to be set after update")
	}

	if cachedRG.CoverArtID.Int64 != coverArt.ID {
		t.Errorf("CoverArtID = %d, want %d", cachedRG.CoverArtID.Int64, coverArt.ID)
	}

	// Empty album → invalid NullInt64.
	emptyTags := &metadata.TrackMetadata{Album: ""}
	rgEmpty := lib.resolveReleaseGroup(q, cache, emptyTags, albumArtistCreditID, sql.NullInt64{})

	if rgEmpty.Valid {
		t.Errorf("empty album should return invalid NullInt64, got valid with ID %d", rgEmpty.Int64)
	}
}

func TestResolveReleaseGroup_CacheHit(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)
	cache := newEntityCache()
	q := lib.db.Queries

	// Pre-populate cache with a known release group.
	// Cache key is composite: "albumName\x00artistCreditID" (use -1 for no artist).
	cache.releaseGroups[fmt.Sprintf("%s\x00%d", "Cached Album", int64(-1))] = sqlcgen.ReleaseGroup{
		ID:   42,
		Name: "Cached Album",
	}

	tags := &metadata.TrackMetadata{Album: "Cached Album"}
	rgID := lib.resolveReleaseGroup(q, cache, tags, sql.NullInt64{}, sql.NullInt64{})

	if !rgID.Valid {
		t.Fatal("expected valid release group ID from cache")
	}

	if rgID.Int64 != 42 {
		t.Errorf("resolveReleaseGroup() = %d, want 42 (cached)", rgID.Int64)
	}
}

// ---------------------------------------------------------------------------
// Orphan cleanup test — DB-level
// ---------------------------------------------------------------------------

func TestOrphanDeletion(t *testing.T) {
	t.Parallel()

	_, db := setupTestLibrary(t)
	ctx := context.Background()
	q := db.Queries

	// Seed an artist credit → recording → audio file chain.
	ac, err := q.UpsertArtistCredit(ctx, "Test Artist")
	if err != nil {
		t.Fatalf("upsert artist credit: %v", err)
	}

	rec, err := q.CreateRecordingFull(ctx, sqlcgen.CreateRecordingFullParams{
		Name:           "Test Song",
		ArtistCreditID: ac.ID,
	})
	if err != nil {
		t.Fatalf("create recording: %v", err)
	}

	af, err := q.CreateAudioFile(ctx, sqlcgen.CreateAudioFileParams{
		FilePath:           "/music/test.mp3",
		LengthMilliseconds: 180000,
		FileTypeID:         0,
		RecordingID:        rec.ID,
		Basename:           "test.mp3",
	})
	if err != nil {
		t.Fatalf("create audio file: %v", err)
	}

	// Add FTS search index entry.
	if err := db.InsertSearchIndex(
		af.ID, "/music/test.mp3", "Test Song", "Test Artist", "",
	); err != nil {
		t.Fatalf("insert search index: %v", err)
	}

	// Verify the search index entry exists before deletion.
	results, err := db.SearchFTS("Test Song", 10)
	if err != nil {
		t.Fatalf("search before delete: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("search results before delete = %d, want 1", len(results))
	}

	// Delete audio file — this is the primary orphan cleanup step.
	if err := q.DeleteAudioFile(ctx, af.ID); err != nil {
		t.Fatalf("delete audio file: %v", err)
	}

	// Verify audio file is gone by attempting to query all audio files.
	allFiles, err := q.GetAllAudioFiles(ctx)
	if err != nil {
		t.Fatalf("get all audio files: %v", err)
	}

	if len(allFiles) != 0 {
		t.Errorf("audio files after delete = %d, want 0", len(allFiles))
	}

	// DeleteSearchIndex on contentless FTS5 table (content='') is
	// expected to error.  The production orphan cleanup code in
	// library.go logs this as a warning — the search index entries
	// become stale but harmless (they reference a non-existent
	// audio_file ID, so JOINs return no results).
	// ClearSearchIndex (used during full rescan) handles bulk cleanup.
	// DeleteSearchIndex on contentless FTS5 is expected to error.
	// Not a fatal error — documents the contentless FTS5 limitation.
	err = db.DeleteSearchIndex(af.ID)
	if err == nil {
		t.Log("DeleteSearchIndex succeeded (unexpected for contentless FTS5)")
	}
}

// ---------------------------------------------------------------------------
// Empty/missing metadata tests
// ---------------------------------------------------------------------------

func TestEntityCache_EmptyFields(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)
	cache := newEntityCache()
	q := lib.db.Queries
	metrics := newScanMetrics()

	// Empty artist credit name — documents behavior (creates "" credit).
	ac, err := lib.cachedUpsertArtistCredit(q, cache, "")
	if err != nil {
		t.Fatalf("cachedUpsertArtistCredit with empty name: %v", err)
	}

	if ac.ID == 0 {
		t.Error("expected non-zero ID even for empty artist credit name")
	}

	// Empty album → resolveReleaseGroup returns invalid NullInt64.
	tags := &metadata.TrackMetadata{Album: ""}
	rgID := lib.resolveReleaseGroup(q, cache, tags, sql.NullInt64{}, sql.NullInt64{})

	if rgID.Valid {
		t.Errorf("empty album should return invalid NullInt64, got valid ID %d", rgID.Int64)
	}

	// resolveAlbumArtistCredit with empty AlbumArtist reuses track artist credit.
	trackTags := &metadata.TrackMetadata{
		Artist:      "Queen",
		AlbumArtist: "",
	}

	trackAC, err := lib.cachedUpsertArtistCredit(q, cache, "Queen")
	if err != nil {
		t.Fatalf("upsert track artist credit: %v", err)
	}

	albumACID := lib.resolveAlbumArtistCredit(q, cache, metrics, trackTags, trackAC.ID)
	if !albumACID.Valid {
		t.Fatal("expected valid album artist credit ID when AlbumArtist is empty")
	}

	if albumACID.Int64 != trackAC.ID {
		t.Errorf(
			"empty AlbumArtist should reuse track credit: got %d, want %d",
			albumACID.Int64, trackAC.ID,
		)
	}

	// resolveAlbumArtistCredit when AlbumArtist matches Artist also reuses.
	sameTags := &metadata.TrackMetadata{
		Artist:      "Queen",
		AlbumArtist: "Queen",
	}

	sameACID := lib.resolveAlbumArtistCredit(q, cache, metrics, sameTags, trackAC.ID)
	if sameACID.Int64 != trackAC.ID {
		t.Errorf(
			"matching AlbumArtist should reuse track credit: got %d, want %d",
			sameACID.Int64, trackAC.ID,
		)
	}
}

// ---------------------------------------------------------------------------
// commitBatch + tagging_items bookkeeping (phase 008.3)
// ---------------------------------------------------------------------------

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
