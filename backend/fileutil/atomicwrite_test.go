package fileutil

import (
	"bytes"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// errSimulatedFailure is a sentinel used in callback-error tests.
var errSimulatedFailure = errors.New("simulated write failure")

func TestAtomicWrite_Success(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")

	// Create target with known content and non-default permissions.
	if err := os.WriteFile(target, []byte("original"), 0o755); err != nil { //nolint:mnd
		t.Fatalf("setup: write target: %v", err)
	}

	err := AtomicWrite(slog.Default(), target, func(tmp *os.File) error {
		_, writeErr := tmp.WriteString("replaced")

		return writeErr
	})
	if err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}

	// Verify content was replaced.
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}

	if string(got) != "replaced" {
		t.Errorf("content: got %q, want %q", got, "replaced")
	}

	// Verify permissions preserved.
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}

	if info.Mode().Perm() != 0o755 { //nolint:mnd
		t.Errorf("permissions: got %o, want %o", info.Mode().Perm(), 0o755) //nolint:mnd
	}

	// Verify no temp file remains.
	tmpPath := target + tmpSuffix
	if _, err := os.Stat(tmpPath); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("temp file should not exist, got err: %v", err)
	}
}

func TestAtomicWrite_NewFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "newfile.txt")

	err := AtomicWrite(slog.Default(), target, func(tmp *os.File) error {
		_, writeErr := tmp.WriteString("brand new")

		return writeErr
	})
	if err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}

	if string(got) != "brand new" {
		t.Errorf("content: got %q, want %q", got, "brand new")
	}

	// Default permissions for a new file should be 0644.
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}

	if info.Mode().Perm() != 0o644 { //nolint:mnd
		t.Errorf("permissions: got %o, want %o", info.Mode().Perm(), 0o644) //nolint:mnd
	}

	// No temp file should remain.
	tmpPath := target + tmpSuffix
	if _, err := os.Stat(tmpPath); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("temp file should not exist, got err: %v", err)
	}
}

func TestAtomicWrite_CallbackError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "unchanged.txt")

	original := []byte("keep me")
	if err := os.WriteFile(target, original, 0o644); err != nil { //nolint:mnd
		t.Fatalf("setup: write target: %v", err)
	}

	err := AtomicWrite(slog.Default(), target, func(tmp *os.File) error {
		// Write partial data then return an error.
		_, _ = tmp.WriteString("partial")

		return errSimulatedFailure
	})

	if !errors.Is(err, errSimulatedFailure) {
		t.Fatalf("expected callback error, got: %v", err)
	}

	// Original content must be untouched.
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}

	if !bytes.Equal(got, original) {
		t.Errorf("content: got %q, want %q", got, original)
	}

	// Temp file must be cleaned up.
	tmpPath := target + tmpSuffix
	if _, err := os.Stat(tmpPath); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("temp file should not exist after callback error, got err: %v", err)
	}
}

func TestAtomicWrite_OrphanCleanup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "song.mp3")
	tmpPath := target + tmpSuffix

	// Simulate orphaned temp file from a previous crash.
	if err := os.WriteFile(tmpPath, []byte("orphaned data"), 0o644); err != nil { //nolint:mnd
		t.Fatalf("setup: create orphan: %v", err)
	}

	err := AtomicWrite(slog.Default(), target, func(tmp *os.File) error {
		_, writeErr := tmp.WriteString("real data")

		return writeErr
	})
	if err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}

	// Target should have the correct content.
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}

	if string(got) != "real data" {
		t.Errorf("content: got %q, want %q", got, "real data")
	}

	// No temp file should remain.
	if _, err := os.Stat(tmpPath); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("orphan temp file should have been cleaned up, got err: %v", err)
	}
}

func TestAtomicWrite_SameDirectoryTempFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	subdir := filepath.Join(dir, "music")

	if err := os.MkdirAll(subdir, 0o755); err != nil { //nolint:mnd
		t.Fatalf("setup: mkdir: %v", err)
	}

	target := filepath.Join(subdir, "track.flac")
	expectedTmp := target + tmpSuffix

	var observedTmpPath string

	err := AtomicWrite(slog.Default(), target, func(tmp *os.File) error {
		observedTmpPath = tmp.Name()
		_, writeErr := tmp.WriteString("flac data")

		return writeErr
	})
	if err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}

	// Verify the temp file was created in the same directory as the target.
	if observedTmpPath != expectedTmp {
		t.Errorf("temp path: got %q, want %q", observedTmpPath, expectedTmp)
	}

	// The temp file's directory must match the target's directory.
	if filepath.Dir(observedTmpPath) != filepath.Dir(target) {
		t.Errorf(
			"temp dir %q differs from target dir %q — cross-device rename would fail",
			filepath.Dir(observedTmpPath), filepath.Dir(target),
		)
	}
}

func TestAtomicWrite_PermissionPreservation(t *testing.T) {
	t.Parallel()

	modes := []fs.FileMode{0o644, 0o755, 0o600} //nolint:mnd

	for _, mode := range modes {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			target := filepath.Join(dir, "file.dat")

			if err := os.WriteFile(target, []byte("old"), mode); err != nil {
				t.Fatalf("setup: write target: %v", err)
			}

			err := AtomicWrite(slog.Default(), target, func(tmp *os.File) error {
				_, writeErr := tmp.WriteString("new")

				return writeErr
			})
			if err != nil {
				t.Fatalf("AtomicWrite: %v", err)
			}

			info, err := os.Stat(target)
			if err != nil {
				t.Fatalf("stat target: %v", err)
			}

			if info.Mode().Perm() != mode {
				t.Errorf("permissions: got %o, want %o", info.Mode().Perm(), mode)
			}
		})
	}
}

func TestAtomicWrite_SyncAndClose(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "large.bin")

	// Write 1 MiB of data.
	const size = 1 << 20 //nolint:mnd
	data := bytes.Repeat([]byte("x"), size)

	err := AtomicWrite(slog.Default(), target, func(tmp *os.File) error {
		_, writeErr := tmp.Write(data)

		return writeErr
	})
	if err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}

	if info.Size() != size {
		t.Errorf("file size: got %d, want %d", info.Size(), size)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Errorf("content mismatch for %d byte file", size)
	}
}
