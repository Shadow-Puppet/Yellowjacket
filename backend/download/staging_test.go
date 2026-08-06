package download

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestStaging(t *testing.T) *Staging {
	t.Helper()

	s, err := NewStagingAt(t.TempDir(), slogDiscard())
	if err != nil {
		t.Fatalf("NewStagingAt: %v", err)
	}

	return s
}

func TestStagingReserveAndRelease(t *testing.T) {
	t.Parallel()

	s := newTestStaging(t)

	dir, err := s.Reserve("item-1")
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("staging dir not created: %v", err)
	}

	if err := s.Release(dir); err != nil {
		t.Fatalf("Release: %v", err)
	}

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("staging dir still exists after Release")
	}
}

// A provider reporting a path outside its staging directory is either
// buggy or hostile.  Either way the import must refuse to follow it,
// because the next step moves those paths into the library.
func TestStagingVerifyRejectsEscape(t *testing.T) {
	t.Parallel()

	s := newTestStaging(t)

	dir, err := s.Reserve("item-1")
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	outside := filepath.Join(s.Root(), "elsewhere.flac")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	tests := []string{
		"../elsewhere.flac",
		outside,
		filepath.Join(dir, "..", "elsewhere.flac"),
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			if _, err := s.Verify(dir, []string{path}); !errors.Is(
				err, ErrEscapesStaging,
			) {
				t.Errorf("Verify(%q) error = %v, want ErrEscapesStaging", path, err)
			}
		})
	}
}

func TestStagingReleaseRejectsOutsideRoot(t *testing.T) {
	t.Parallel()

	s := newTestStaging(t)

	other := t.TempDir()

	if err := s.Release(other); !errors.Is(err, ErrEscapesStaging) {
		t.Errorf("Release outside root error = %v, want ErrEscapesStaging", err)
	}

	if _, err := os.Stat(other); err != nil {
		t.Error("Release removed a directory outside the staging root")
	}
}

func TestStagingVerifySkipsEmptyFiles(t *testing.T) {
	t.Parallel()

	s := newTestStaging(t)

	dir, err := s.Reserve("item-1")
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	good := filepath.Join(dir, "good.flac")
	empty := filepath.Join(dir, "empty.flac")

	if err := os.WriteFile(good, []byte("data"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	files, err := s.Verify(dir, []string{good, empty})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if len(files) != 1 || files[0] != good {
		t.Errorf("Verify = %v, want just %s", files, good)
	}
}

func TestStagingSweepKeepsRecentDirs(t *testing.T) {
	t.Parallel()

	s := newTestStaging(t)

	recent, err := s.Reserve("recent")
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	stale, err := s.Reserve("stale")
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	old := time.Now().Add(-staleAge - time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	removed, err := s.Sweep()
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}

	if _, err := os.Stat(recent); err != nil {
		t.Error("sweep removed a recent staging dir")
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("sweep kept a stale staging dir")
	}
}

func TestStagingSweepOrphans(t *testing.T) {
	t.Parallel()

	s := newTestStaging(t)

	live, err := s.Reserve("live")
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	orphan, err := s.Reserve("orphan")
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	removed, err := s.SweepOrphans(map[string]bool{"live": true})
	if err != nil {
		t.Fatalf("SweepOrphans: %v", err)
	}

	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}

	if _, err := os.Stat(live); err != nil {
		t.Error("sweep removed a live staging dir")
	}

	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Error("sweep kept an orphaned staging dir")
	}
}
