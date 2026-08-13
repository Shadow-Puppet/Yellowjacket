package library

import (
	"errors"
	"fmt"

	"yellowjacket/backend/events"
)

// errNoPathsToRemove is returned when RemoveFromLibrary is called with
// nothing to remove.  A static error because err113 forbids a dynamic
// one, and a sentinel because the frontend distinguishes it.
var errNoPathsToRemove = errors.New("no file paths given to remove")

// RemovalResult reports what one RemoveFromLibrary call did.
type RemovalResult struct {
	// TracksRemoved is how many audio_files rows were deleted.  It can
	// be lower than len(filePaths) if a path was already gone.
	TracksRemoved int64 `json:"tracksRemoved"`
	// PathsExcluded is how many paths the scanner will now skip.
	PathsExcluded int64 `json:"pathsExcluded"`
}

// RemoveFromLibrary deletes the database rows for the given file paths
// and records each path as excluded, so the next scan does not import
// it again.  **It does not touch the files on disk** — that is the
// promise the confirmation dialog makes, and the reason this operation
// is safe to put one keystroke from a focused row.
//
// The exclusion is not an enhancement.  Without it the next scan finds
// the file, sees no row, and imports it again — a button that undoes
// itself, which is worse than no button.
func (l *Library) RemoveFromLibrary(filePaths []string) (*RemovalResult, error) {
	if len(filePaths) == 0 {
		return nil, errNoPathsToRemove
	}

	rows, err := l.db.Queries.GetAudioFilesByPaths(l.ctx, filePaths)
	if err != nil {
		return nil, fmt.Errorf("could not resolve paths to remove: %w", err)
	}

	tx, err := l.db.BeginTx()
	if err != nil {
		return nil, fmt.Errorf("could not begin removal transaction: %w", err)
	}

	committed := false

	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	txq := l.db.Queries.WithTx(tx)

	var result RemovalResult

	// Exclude every path the caller named, including one whose row has
	// already gone: the user asked for that file to stay out, and a row
	// that disappeared between the click and the commit is not a reason
	// to let the next scan bring it back.  A path with no row at all is
	// attributed to the library that contains it, resolved below.
	rowByPath := make(map[string]int64, len(rows))

	for _, row := range rows {
		rowByPath[row.FilePath] = row.LibraryID

		if err := txq.DeleteAudioFile(l.ctx, row.ID); err != nil {
			return nil, fmt.Errorf("could not delete audio file row: %w", err)
		}

		result.TracksRemoved++

		// Keep the file's tagging group in sync, exactly as the scan's
		// orphan cleanup does: drop the count and clear the group once
		// it is empty, or the autotag queue keeps a row counting files
		// that no longer exist.
		if row.GroupKey != "" {
			if err := txq.DecrementTaggingItemTrackCount(l.ctx, row.GroupKey); err != nil {
				return nil, fmt.Errorf("could not decrement tagging group: %w", err)
			}

			if err := txq.DeleteTaggingItemIfEmpty(l.ctx, row.GroupKey); err != nil {
				return nil, fmt.Errorf("could not clear emptied tagging group: %w", err)
			}
		}
	}

	libraryIDs, err := l.libraryIDsForPaths(filePaths, rowByPath)
	if err != nil {
		return nil, err
	}

	for _, path := range filePaths {
		libraryID, known := libraryIDs[path]
		if !known {
			// Outside every configured library: no scan will ever
			// visit it, so there is nothing to exclude it from.
			l.logger.Debug("removal: path belongs to no library, not excluding",
				"path", path)

			continue
		}

		if err := txq.ExcludePath(l.ctx, excludeParams(libraryID, path)); err != nil {
			return nil, fmt.Errorf("could not exclude path: %w", err)
		}

		result.PathsExcluded++
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("could not commit removal: %w", err)
	}

	committed = true

	// Post-commit, and best-effort: the rows are gone either way, and a
	// failure here leaves stale index entries rather than a wrong
	// library.  The FTS5 index is contentless, so a stale entry is
	// harmless — searches join track_metadata, which no longer has the
	// row — but removing it keeps the index from growing forever.
	for _, row := range rows {
		if err := l.db.DeleteSearchIndex(row.ID); err != nil {
			l.logger.Warn("could not delete FTS entry for removed track",
				"path", row.FilePath, "id", row.ID, "err", err)
		}
	}

	// An album, artist or genre whose last track just went is now a row
	// with nothing behind it, and the album list selects from
	// release_groups rather than from audio_files — so it would keep
	// rendering an album the user has no tracks of.
	l.pruneOrphanedMetadata()

	l.emit(events.TracksRemovedFromLibrary, map[string]any{
		"filePaths": filePaths,
		"count":     result.TracksRemoved,
	})

	l.logger.Info("removed tracks from library",
		"requested", len(filePaths),
		"rowsDeleted", result.TracksRemoved,
		"pathsExcluded", result.PathsExcluded,
	)

	return &result, nil
}

// libraryIDsForPaths maps each path to the library it belongs to.  A
// path that still had a row takes that row's library_id; one that did
// not is matched against the configured library roots by prefix, which
// is what the scan walk would do with it.
func (l *Library) libraryIDsForPaths(
	filePaths []string,
	rowByPath map[string]int64,
) (map[string]int64, error) {
	out := make(map[string]int64, len(filePaths))

	var libs []libraryRoot

	for _, path := range filePaths {
		if libraryID, ok := rowByPath[path]; ok {
			out[path] = libraryID

			continue
		}

		if libs == nil {
			var err error

			libs, err = l.libraryRoots()
			if err != nil {
				return nil, err
			}
		}

		for _, lib := range libs {
			if pathWithin(lib.path, path) {
				out[path] = lib.id

				break
			}
		}
	}

	return out, nil
}
