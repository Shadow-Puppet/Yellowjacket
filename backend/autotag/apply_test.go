package autotag_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"yellowjacket/backend/autotag"
	"yellowjacket/backend/database"
	"yellowjacket/backend/database/sql/sqlcgen"
)

// recordingTagWriter is a TagWriter stub that records each call's
// path + changes for assertion.  Returns nil from
// WriteTrackTagsByPath unless writeErr is set, in which case every
// call returns it.
type recordingTagWriter struct {
	mu       sync.Mutex
	calls    []recordedTagWrite
	writeErr error
}

type recordedTagWrite struct {
	filePath string
	changes  autotag.TagChanges
}

func (w *recordingTagWriter) WriteTrackTagsByPath(
	filePath string, changes autotag.TagChanges,
) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.calls = append(w.calls, recordedTagWrite{filePath: filePath, changes: changes})

	return w.writeErr
}

// stubCoverArt is a CoverArtEmbedder stub that counts calls so we
// can verify FetchArt runs exactly once per Apply.  hasEmbedded
// is the response HasEmbeddedArt returns for every file; art is
// what FetchArt returns.
type stubCoverArt struct {
	mu            sync.Mutex
	fetchCalls    int
	embeddedCalls int
	art           []byte
	hasEmbedded   bool
	releaseSeen   string
}

func (s *stubCoverArt) FetchArt(_ context.Context, releaseGroupMBID string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.fetchCalls++
	s.releaseSeen = releaseGroupMBID

	return s.art, nil
}

func (s *stubCoverArt) HasEmbeddedArt(_ string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.embeddedCalls++

	return s.hasEmbedded
}

// silentLogger swallows log output during tests.
func silentLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// seedAudioFiles inserts the minimum DB rows the apply pipeline
// touches: artist credit, recordings, release group, RG-recording
// links, and audio_files.  Returns the audio_file IDs in track
// order so the test can build matching ApplyPlan entries.
func seedAudioFiles(
	t *testing.T, db *database.DB, groupKey string, paths []string,
) []sqlcgen.AudioFile {
	t.Helper()

	q := db.Queries
	ctx := db.Ctx

	out := make([]sqlcgen.AudioFile, 0, len(paths))

	for i, p := range paths {
		id := database.InsertTestTrack(t, db, database.TestTrack{
			FilePath:    p,
			Title:       p,
			Artist:      "Test Artist",
			Album:       "Test Album",
			TrackNumber: int64(i + 1),
			LengthMs:    100000,
			GroupKey:    groupKey,
		})

		af, err := q.GetAudioFile(ctx, id)
		if err != nil {
			t.Fatalf("read seeded audio file: %v", err)
		}

		out = append(out, af)
	}

	if _, err := db.ExecContext(`
		INSERT INTO tagging_items (group_key, library_id, track_count, album_name, album_artist, disc_number, status)
		VALUES (?, 0, ?, 'Test Album', 'Test Artist', 0, 'pending')
	`, groupKey, len(paths)); err != nil {
		t.Fatalf("insert tagging_item: %v", err)
	}

	return out
}

// TestApply_SkipsNoOpChanges verifies that a track whose Changes
// map is empty (nothing would change in the file) doesn't trigger
// a WriteTrackTagsByPath call but still counts as a successful
// track in the result.  Today the writer would return errNoChanges
// for an empty changes map; the apply would record a spurious
// failure.  This test pins the new behaviour: skip cleanly.
func TestApply_SkipsNoOpChanges(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	files := seedAudioFiles(t, db, "g-noop", []string{"/tmp/a.mp3", "/tmp/b.mp3"})

	tw := &recordingTagWriter{}
	cover := &stubCoverArt{} // no art to embed

	applier := autotag.NewApplier(db.Queries, tw, cover, silentLogger())

	plan := &autotag.ApplyPlan{
		GroupKey:  "g-noop",
		Candidate: autotag.Candidate{Title: "Test Album"}, // no RG MBID → no cover fetch
		Tracks: []autotag.TrackApply{
			{
				Local: autotag.LocalTrack{
					AudioFileID: files[0].ID, FilePath: "/tmp/a.mp3",
				},
				Changes: autotag.TagChanges{},
				Aligned: true,
			},
			{
				Local: autotag.LocalTrack{
					AudioFileID: files[1].ID, FilePath: "/tmp/b.mp3",
				},
				Changes: autotag.TagChanges{autotag.FieldTitle: "New"},
				Aligned: true,
			},
		},
	}

	result, err := applier.Apply(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	if result.Succeeded != 2 { //nolint:mnd
		t.Errorf("succeeded = %d, want 2", result.Succeeded)
	}

	if result.Failed != 0 {
		t.Errorf("failed = %d, want 0", result.Failed)
	}

	if len(tw.calls) != 1 {
		t.Errorf("WriteTrackTagsByPath calls = %d, want 1 (no-op skipped)", len(tw.calls))
	}

	if len(tw.calls) > 0 && tw.calls[0].filePath != "/tmp/b.mp3" {
		t.Errorf("call 0 path = %q, want /tmp/b.mp3 (the only one with changes)",
			tw.calls[0].filePath)
	}
}

// TestApply_FetchArtCalledOncePerAlbum verifies that the cover-art
// network fetch happens exactly once per Apply, regardless of how
// many tracks need it.  HasEmbeddedArt is still called per track
// so we never overwrite existing art.
func TestApply_FetchArtCalledOncePerAlbum(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	paths := []string{"/tmp/x1.mp3", "/tmp/x2.mp3", "/tmp/x3.mp3", "/tmp/x4.mp3"}
	files := seedAudioFiles(t, db, "g-art", paths)

	tw := &recordingTagWriter{}
	cover := &stubCoverArt{art: []byte("fake-jpeg")} // network bytes available

	applier := autotag.NewApplier(db.Queries, tw, cover, silentLogger())

	tracks := make([]autotag.TrackApply, 0, len(paths))
	for i, p := range paths {
		tracks = append(tracks, autotag.TrackApply{
			Local: autotag.LocalTrack{
				AudioFileID: files[i].ID, FilePath: p,
			},
			Changes: autotag.TagChanges{autotag.FieldTitle: "T"},
			Aligned: true,
		})
	}

	plan := &autotag.ApplyPlan{
		GroupKey: "g-art",
		Candidate: autotag.Candidate{
			Title:            "Test Album",
			ReleaseGroupMBID: "rg-fake",
		},
		Tracks: tracks,
	}

	result, err := applier.Apply(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	if result.Succeeded != len(paths) {
		t.Errorf("succeeded = %d, want %d", result.Succeeded, len(paths))
	}

	if cover.fetchCalls != 1 {
		t.Errorf("FetchArt calls = %d, want 1 (memoised per album)", cover.fetchCalls)
	}

	if cover.embeddedCalls != len(paths) {
		t.Errorf("HasEmbeddedArt calls = %d, want %d (one per track)",
			cover.embeddedCalls, len(paths))
	}

	if cover.releaseSeen != "rg-fake" {
		t.Errorf("FetchArt rg = %q, want rg-fake", cover.releaseSeen)
	}

	// Every track's WriteTrackTagsByPath call should carry the
	// cover-art bytes since none of the files had embedded art.
	for i, c := range tw.calls {
		if _, ok := c.changes[autotag.FieldCoverArt]; !ok {
			t.Errorf("call %d: no cover_art in changes", i)
		}
	}
}

// TestApply_SkipsCoverWhenAlreadyEmbedded verifies that
// HasEmbeddedArt=true blocks the per-track cover-art merge — never
// replace existing art is the invariant.
func TestApply_SkipsCoverWhenAlreadyEmbedded(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	files := seedAudioFiles(t, db, "g-emb", []string{"/tmp/y1.mp3"})

	tw := &recordingTagWriter{}
	// File reports it already has embedded art; FetchArt still
	// runs (one call, then memoised) but its bytes never land in
	// the change map.
	cover := &stubCoverArt{art: []byte("bytes"), hasEmbedded: true}

	applier := autotag.NewApplier(db.Queries, tw, cover, silentLogger())

	plan := &autotag.ApplyPlan{
		GroupKey: "g-emb",
		Candidate: autotag.Candidate{
			Title:            "Test Album",
			ReleaseGroupMBID: "rg-emb",
		},
		Tracks: []autotag.TrackApply{
			{
				Local: autotag.LocalTrack{
					AudioFileID: files[0].ID, FilePath: "/tmp/y1.mp3",
				},
				Changes: autotag.TagChanges{autotag.FieldTitle: "T"},
				Aligned: true,
			},
		},
	}

	if _, err := applier.Apply(context.Background(), plan, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if len(tw.calls) != 1 {
		t.Fatalf("write calls = %d, want 1", len(tw.calls))
	}

	if _, ok := tw.calls[0].changes[autotag.FieldCoverArt]; ok {
		t.Errorf("cover_art present in changes; should have been skipped")
	}
}

// TestApply_ProgressCallbackInvocations verifies onProgress is
// called once per aligned track and that current/total reflect
// the position in the sequence.
func TestApply_ProgressCallbackInvocations(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	paths := []string{"/tmp/p1.mp3", "/tmp/p2.mp3", "/tmp/p3.mp3"}
	files := seedAudioFiles(t, db, "g-prog", paths)

	applier := autotag.NewApplier(db.Queries, &recordingTagWriter{}, nil, silentLogger())

	tracks := make([]autotag.TrackApply, 0, len(paths))
	for i, p := range paths {
		tracks = append(tracks, autotag.TrackApply{
			Local: autotag.LocalTrack{
				AudioFileID: files[i].ID, FilePath: p,
			},
			Changes: autotag.TagChanges{autotag.FieldTitle: "T"},
			Aligned: true,
		})
	}

	plan := &autotag.ApplyPlan{
		GroupKey:  "g-prog",
		Candidate: autotag.Candidate{Title: "Test Album"},
		Tracks:    tracks,
	}

	type call struct {
		current, total, succeeded, failed int
	}

	var calls []call

	if _, err := applier.Apply(
		context.Background(),
		plan,
		func(current, total, succeeded, failed int) {
			calls = append(calls, call{current, total, succeeded, failed})
		},
	); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if len(calls) != len(paths) {
		t.Fatalf("progress callbacks = %d, want %d", len(calls), len(paths))
	}

	for i, c := range calls {
		if c.current != i+1 {
			t.Errorf("call %d: current = %d, want %d", i, c.current, i+1)
		}

		if c.total != len(paths) {
			t.Errorf("call %d: total = %d, want %d", i, c.total, len(paths))
		}
	}
}
