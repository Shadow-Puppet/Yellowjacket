package download

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"yellowjacket/backend/system"
)

// Downloads land here first and only move into the library once they
// have been verified and tagged.  The reason is not tidiness: the
// library scanner watches library paths, and a half-written file or a
// mislabelled Soulseek folder that lands there gets ingested, indexed
// and surfaced to the user before anyone can check it.  Staging makes
// the import step the single writer into library paths.

// stagingDirName is the staging root inside the user data directory.
const stagingDirName = "downloads"

// staleAge is how long an abandoned staging directory survives before
// the startup sweep removes it.  Long enough that a download
// interrupted by a crash can still be inspected; short enough that a
// failed grab does not sit on disk forever.
const staleAge = 48 * time.Hour

// ErrEscapesStaging is returned when a provider reports a file path
// outside the directory it was given.
var ErrEscapesStaging = errors.New("path escapes the staging directory")

// Staging owns the download staging area.
type Staging struct {
	root   string
	logger *slog.Logger
}

// NewStaging creates the staging area under the user data directory.
func NewStaging(logger *slog.Logger) (*Staging, error) {
	dir, err := system.GetUserDataDirPath()
	if err != nil {
		return nil, fmt.Errorf("resolve user data dir: %w", err)
	}

	return NewStagingAt(filepath.Join(dir, stagingDirName), logger)
}

// NewStagingAt creates a staging area at an explicit root.
func NewStagingAt(root string, logger *slog.Logger) (*Staging, error) {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("create staging root: %w", err)
	}

	return &Staging{root: root, logger: logger}, nil
}

// Root returns the staging root directory.
func (s *Staging) Root() string {
	return s.root
}

// Reserve creates and returns a directory for one download item.
func (s *Staging) Reserve(itemID string) (string, error) {
	dir := filepath.Join(s.root, sanitizeSegment(itemID))

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create staging dir: %w", err)
	}

	return dir, nil
}

// Release removes a download item's staging directory and everything
// in it.  Called after a successful import and after a failed grab.
func (s *Staging) Release(dir string) error {
	if !s.contains(dir) {
		return fmt.Errorf("%w: %s", ErrEscapesStaging, dir)
	}

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove staging dir: %w", err)
	}

	return nil
}

// contains reports whether dir is inside the staging root.  Guards
// every destructive operation, because dir ultimately comes from a
// database row a provider wrote.
func (s *Staging) contains(dir string) bool {
	absRoot, err := filepath.Abs(s.root)
	if err != nil {
		return false
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}

	rel, err := filepath.Rel(absRoot, absDir)
	if err != nil {
		return false
	}

	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Verify checks that every path a provider reported is a real file
// inside dir, and returns them cleaned.  A transport that reports a
// path outside its directory is either buggy or hostile; either way the
// import must not follow it.
func (s *Staging) Verify(dir string, files []string) ([]string, error) {
	if !s.contains(dir) {
		return nil, fmt.Errorf("%w: %s", ErrEscapesStaging, dir)
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve staging dir: %w", err)
	}

	out := make([]string, 0, len(files))

	for _, f := range files {
		abs := f
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(absDir, f)
		}

		abs = filepath.Clean(abs)

		rel, err := filepath.Rel(absDir, abs)
		if err != nil ||
			rel == ".." ||
			strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("%w: %s", ErrEscapesStaging, f)
		}

		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("stat downloaded file %s: %w", rel, err)
		}

		if info.IsDir() || info.Size() == 0 {
			continue
		}

		out = append(out, abs)
	}

	return out, nil
}

// Sweep removes staging directories left behind by a previous run.
// Anything still present at startup belongs to a download that did not
// finish, since a completed import releases its directory.
//
// Directories younger than staleAge are kept: a grab may legitimately
// be resumed, and deleting a partial transfer the user is waiting on
// would be worse than leaving a few megabytes on disk.
func (s *Staging) Sweep() (removed int, err error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}

		return 0, fmt.Errorf("read staging root: %w", err)
	}

	cutoff := time.Now().Add(-staleAge)

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}

		if info.ModTime().After(cutoff) {
			continue
		}

		dir := filepath.Join(s.root, e.Name())

		if err := os.RemoveAll(dir); err != nil {
			s.logger.Warn(
				"could not remove stale staging directory",
				"dir", dir,
				"error", err,
			)

			continue
		}

		removed++
	}

	if removed > 0 {
		s.logger.Info("removed stale staging directories", "count", removed)
	}

	return removed, nil
}

// SweepOrphans removes staging directories whose item IDs are not in
// the live set.  Called after the item store is loaded, so a directory
// belonging to a download the database no longer knows about goes away
// even if it is recent.
func (s *Staging) SweepOrphans(live map[string]bool) (removed int, err error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}

		return 0, fmt.Errorf("read staging root: %w", err)
	}

	for _, e := range entries {
		if !e.IsDir() || live[e.Name()] {
			continue
		}

		if err := os.RemoveAll(filepath.Join(s.root, e.Name())); err != nil {
			s.logger.Warn(
				"could not remove orphaned staging directory",
				"dir", e.Name(),
				"error", err,
			)

			continue
		}

		removed++
	}

	return removed, nil
}

// sanitizeSegment reduces a string to a safe single path segment.
func sanitizeSegment(s string) string {
	var b strings.Builder

	b.Grow(len(s))

	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}

	out := b.String()
	if out == "" {
		return "item"
	}

	return out
}
