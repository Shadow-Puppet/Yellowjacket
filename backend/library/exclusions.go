package library

import (
	"fmt"
	"path/filepath"
	"strings"

	"yellowjacket/backend/database/sql/sqlcgen"
)

// libraryRoot is one configured library's id and root directory.
type libraryRoot struct {
	id   int64
	path string
}

// libraryRoots returns every configured library's id and root path.
func (l *Library) libraryRoots() ([]libraryRoot, error) {
	libs, err := l.db.Queries.GetAllLibraries(l.ctx)
	if err != nil {
		return nil, fmt.Errorf("could not load libraries: %w", err)
	}

	roots := make([]libraryRoot, 0, len(libs))
	for _, lib := range libs {
		roots = append(roots, libraryRoot{id: lib.ID, path: lib.Path})
	}

	return roots, nil
}

// pathWithin reports whether path lies under root.  Both are cleaned
// first, and the comparison keeps the separator so /music/rock does not
// swallow /music/rockabilly.
func pathWithin(root, path string) bool {
	cleanRoot := filepath.Clean(root)
	cleanPath := filepath.Clean(path)

	if cleanRoot == cleanPath {
		return true
	}

	return strings.HasPrefix(cleanPath, cleanRoot+string(filepath.Separator))
}

// excludeParams is the insert parameter for one exclusion.
func excludeParams(libraryID int64, filePath string) sqlcgen.ExcludePathParams {
	return sqlcgen.ExcludePathParams{
		LibraryID: libraryID,
		FilePath:  filePath,
	}
}

// excludedPathSet loads a library's excluded paths as a set, cleaned
// the same way the walk builds its absolute paths so the two compare.
//
// A failure here returns an empty set and logs: a scan that cannot read
// the exclusions imports what it finds, which is the pre-exclusion
// behaviour rather than an empty library.
func (l *Library) excludedPathSet(libraryID int64) map[string]struct{} {
	paths, err := l.db.Queries.GetExcludedPathsByLibrary(l.ctx, libraryID)
	if err != nil {
		l.logger.Warn("could not load excluded paths, scanning everything",
			"libraryID", libraryID, "err", err)

		return nil
	}

	if len(paths) == 0 {
		return nil
	}

	set := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		set[filepath.Clean(p)] = struct{}{}
	}

	return set
}

// isExcluded reports whether an absolute file path is in the set.  A
// nil set excludes nothing, which is what every caller wants when
// there are no exclusions at all.
func isExcluded(set map[string]struct{}, absolutePath string) bool {
	if len(set) == 0 {
		return false
	}

	_, found := set[filepath.Clean(absolutePath)]

	return found
}
