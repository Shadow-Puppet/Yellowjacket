// Package fileutil provides file system utilities for safe file operations.
package fileutil

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"syscall"
)

// ErrCrossDevice is returned when an atomic rename fails because the temp file
// and target reside on different filesystems. There is no copy-then-delete
// fallback — callers must ensure both paths share the same mount point.
var ErrCrossDevice = errors.New("atomic write: cross-device rename not supported")

// tmpSuffix is the deterministic extension appended to the target path for the
// temporary file. A fixed suffix (rather than a random one) makes orphan
// cleanup straightforward.
const tmpSuffix = ".yj-tmp"

// AtomicWrite writes to targetPath atomically. It creates a temporary file in
// the same directory as targetPath (with a ".yj-tmp" suffix), passes it to fn
// for writing, syncs and closes the file, then renames it over targetPath. If
// the target already exists its permission bits are preserved; otherwise the
// new file receives mode 0644.
//
// Before writing, any orphaned .yj-tmp file from a previous interrupted
// operation is removed. If removal fails it is logged at debug level and the
// write proceeds.
//
// On any error after the temp file is created the temp file is removed.
func AtomicWrite(logger *slog.Logger, targetPath string, fn func(tmp *os.File) error) (err error) {
	tmpPath := targetPath + tmpSuffix

	// --- 1. Clean orphaned temp file from a previous crash ----------------
	if _, statErr := os.Lstat(tmpPath); statErr == nil {
		if rmErr := os.Remove(tmpPath); rmErr != nil {
			logger.Debug("could not remove orphaned temp file",
				slog.String("path", tmpPath),
				slog.String("err", rmErr.Error()),
			)
		}
	}

	// --- 2. Read target permissions (if target exists) --------------------
	mode := fs.FileMode(0o644) //nolint:mnd // default for new files

	if info, statErr := os.Stat(targetPath); statErr == nil {
		mode = info.Mode().Perm()
	}

	// --- 3. Create temp file ---------------------------------------------
	tmp, createErr := os.Create(tmpPath)
	if createErr != nil {
		return fmt.Errorf("atomic write: create temp: %w", createErr)
	}

	// Ensure temp file is removed on any failure path after creation.
	defer func() {
		if err != nil {
			_ = os.Remove(tmpPath)
		}
	}()

	// --- 4. Caller writes data -------------------------------------------
	if err = fn(tmp); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("atomic write: callback: %w", err)
	}

	// --- 5. Sync for durability ------------------------------------------
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("atomic write: sync: %w", err)
	}

	// --- 6. Close --------------------------------------------------------
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("atomic write: close: %w", err)
	}

	// --- 7. Preserve permissions -----------------------------------------
	if err = os.Chmod(tmpPath, mode); err != nil {
		return fmt.Errorf("atomic write: chmod: %w", err)
	}

	// --- 8. Atomic rename ------------------------------------------------
	if err = os.Rename(tmpPath, targetPath); err != nil {
		if errors.Is(err, syscall.EXDEV) {
			return fmt.Errorf("%w: %w", ErrCrossDevice, err)
		}

		return fmt.Errorf("atomic write: rename: %w", err)
	}

	return nil
}
