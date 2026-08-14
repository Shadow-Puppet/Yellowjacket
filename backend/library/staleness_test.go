package library

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"yellowjacket/backend/database/sql/sqlcgen"
)

// ---------------------------------------------------------------------------
// fileContentChanged — the staleness predicate
// ---------------------------------------------------------------------------

func TestFileContentChanged(t *testing.T) {
	t.Parallel()

	const (
		baseMod  int64 = 1700000000
		baseSize int64 = 5_000_000
	)

	tests := []struct {
		name        string
		recordedMod int64
		recordedSz  int64
		diskMod     int64
		diskSz      int64
		want        bool
	}{
		{
			name:        "unchanged file",
			recordedMod: baseMod,
			recordedSz:  baseSize,
			diskMod:     baseMod,
			diskSz:      baseSize,
			want:        false,
		},
		{
			name:        "retagged in place, mtime bumped and size grew",
			recordedMod: baseMod,
			recordedSz:  baseSize,
			diskMod:     baseMod + 60,
			diskSz:      baseSize + 2048,
			want:        true,
		},
		{
			name:        "mtime bumped, size absorbed by tag padding",
			recordedMod: baseMod,
			recordedSz:  baseSize,
			diskMod:     baseMod + 60,
			diskSz:      baseSize,
			want:        true,
		},
		{
			name:        "size changed but mtime preserved by the writer",
			recordedMod: baseMod,
			recordedSz:  baseSize,
			diskMod:     baseMod,
			diskSz:      baseSize + 2048,
			want:        true,
		},
		{
			name:        "no recorded baseline is never stale",
			recordedMod: 0,
			recordedSz:  baseSize,
			diskMod:     baseMod,
			diskSz:      baseSize + 4096,
			want:        false,
		},
		{
			name:        "failed stat is never stale",
			recordedMod: baseMod,
			recordedSz:  baseSize,
			diskMod:     0,
			diskSz:      0,
			want:        false,
		},
		{
			name:        "file replaced with an older copy",
			recordedMod: baseMod,
			recordedSz:  baseSize,
			diskMod:     baseMod - 3600,
			diskSz:      baseSize,
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			af := sqlcgen.AudioFile{
				ModifiedAt: tt.recordedMod,
				FileSize:   tt.recordedSz,
			}

			got := fileContentChanged(af, tt.diskMod, tt.diskSz)
			if got != tt.want {
				t.Errorf(
					"fileContentChanged() = %v, want %v",
					got, tt.want,
				)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// surveyAudioFiles — soft scan change signal
// ---------------------------------------------------------------------------

func TestSurveyAudioFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Two audio files plus one unsupported file that must be ignored.
	writeFile(t, filepath.Join(dir, "a.mp3"), 1024)
	writeFile(t, filepath.Join(dir, "nested", "b.flac"), 2048)
	writeFile(t, filepath.Join(dir, "cover.jpg"), 512)

	older := time.Now().Add(-48 * time.Hour)
	newer := time.Now().Add(-1 * time.Hour)

	setModTime(t, filepath.Join(dir, "a.mp3"), older)
	setModTime(t, filepath.Join(dir, "nested", "b.flac"), newer)
	// The ignored file is the newest on disk — it must not influence
	// the result, or every artwork change would trigger a rescan.
	setModTime(t, filepath.Join(dir, "cover.jpg"), time.Now())

	count, maxMod := surveyAudioFiles(dir, nil)

	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}

	if maxMod != newer.Unix() {
		t.Errorf("maxModTime = %d, want %d", maxMod, newer.Unix())
	}

	// Retagging the older file in place makes it the newest, which is
	// what the soft scan compares against the database.
	touched := time.Now()
	setModTime(t, filepath.Join(dir, "a.mp3"), touched)

	_, afterMod := surveyAudioFiles(dir, nil)

	if afterMod != touched.Unix() {
		t.Errorf(
			"maxModTime after touch = %d, want %d",
			afterMod, touched.Unix(),
		)
	}
}

func TestSurveyAudioFiles_EmptyDir(t *testing.T) {
	t.Parallel()

	count, maxMod := surveyAudioFiles(t.TempDir(), nil)

	if count != 0 || maxMod != 0 {
		t.Errorf(
			"surveyAudioFiles(empty) = (%d, %d), want (0, 0)",
			count, maxMod,
		)
	}
}

// ---------------------------------------------------------------------------
// flushStatBackfill — baseline backfill for pre-migration rows
// ---------------------------------------------------------------------------

func TestFlushStatBackfill(t *testing.T) {
	t.Parallel()

	lib, db := setupTestLibrary(t)
	ctx := lib.ctx
	q := db.Queries

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

	// Seed two rows with no baseline, as migration 47 leaves them.
	first, err := q.CreateAudioFile(ctx, sqlcgen.CreateAudioFileParams{
		FilePath:           "/music/first.mp3",
		LengthMilliseconds: 180000,
		RecordingID:        rec.ID,
		Basename:           "first.mp3",
	})
	if err != nil {
		t.Fatalf("create first audio file: %v", err)
	}

	second, err := q.CreateAudioFile(ctx, sqlcgen.CreateAudioFileParams{
		FilePath:           "/music/second.mp3",
		LengthMilliseconds: 200000,
		RecordingID:        rec.ID,
		Basename:           "second.mp3",
	})
	if err != nil {
		t.Fatalf("create second audio file: %v", err)
	}

	if first.ModifiedAt != 0 {
		t.Fatalf("seeded ModifiedAt = %d, want 0", first.ModifiedAt)
	}

	lib.flushStatBackfill([]sqlcgen.UpdateAudioFileStatParams{
		{ModifiedAt: 1700000000, FileSize: 4096, ID: first.ID},
		{ModifiedAt: 1700000500, FileSize: 8192, ID: second.ID},
	})

	got, err := q.GetAudioFile(ctx, first.ID)
	if err != nil {
		t.Fatalf("get first audio file: %v", err)
	}

	if got.ModifiedAt != 1700000000 || got.FileSize != 4096 {
		t.Errorf(
			"first row = (mtime %d, size %d), want (1700000000, 4096)",
			got.ModifiedAt, got.FileSize,
		)
	}

	// A backfilled row now has a baseline, so the same file on disk is
	// no longer treated as stale.
	if fileContentChanged(got, 1700000000, 4096) {
		t.Error("backfilled row reported stale against identical stat")
	}

	// The backfill must not disturb unrelated columns.
	if got.LengthMilliseconds != 180000 {
		t.Errorf(
			"LengthMilliseconds = %d, want 180000 (backfill overwrote it)",
			got.LengthMilliseconds,
		)
	}

	if got.FilePath != "/music/first.mp3" {
		t.Errorf("FilePath = %q, want /music/first.mp3", got.FilePath)
	}
}

func TestFlushStatBackfill_Empty(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)

	// Must be a no-op rather than opening an empty transaction.
	lib.flushStatBackfill(nil)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func writeFile(t *testing.T, path string, size int) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}

	if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func setModTime(t *testing.T, path string, mt time.Time) {
	t.Helper()

	if err := os.Chtimes(path, mt, mt); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}
