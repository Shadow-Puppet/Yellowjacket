package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"yellowjacket/backend/database"
	"yellowjacket/backend/database/sql/sqlcgen"
	"yellowjacket/backend/events"
)

// Sentinel errors for library CRUD validation.
var (
	errLibraryNameEmpty     = errors.New("library name must not be empty")
	errLibraryNameTooLong   = errors.New("library name must be 50 characters or fewer")
	errLibraryNameDuplicate = errors.New("a library with that name already exists")
	errLibraryPathNotExist  = errors.New("directory does not exist")
	errQueryNoRows          = errors.New("query returned no rows")
)

// maxLibraryNameLength is the maximum number of characters allowed
// in a library display name.
const maxLibraryNameLength = 50

// RemovalHooks contains callbacks invoked during library removal.
// These break circular dependencies between the library, player,
// and queue packages.
type RemovalHooks struct {
	// StopPlayback stops the currently-playing track.
	StopPlayback func()
	// CompactQueue reloads queue state after cascade deletes.
	CompactQueue func()
	// PostRemove runs after the removal commits, for cross-cutting
	// invalidation (e.g. clearing library-sync "ready" markers).
	PostRemove func()
}

// SetRemovalHooks provides optional hooks for cross-cutting
// orchestration during RemoveLibrary.
func (l *Library) SetRemovalHooks(h RemovalHooks) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.removalHooks = h
}

// RemovalImpact contains pre-removal counts for the confirmation dialog.
type RemovalImpact struct {
	TrackCount        int64 `json:"trackCount"`
	PlaylistsAffected int64 `json:"playlistsAffected"`
	QueueItemCount    int64 `json:"queueItemCount"`
}

// RemovalSummary contains post-removal counts for the toast notification.
type RemovalSummary struct {
	TracksDeleted     int64 `json:"tracksDeleted"`
	ArtistsRemoved    int64 `json:"artistsRemoved"`
	AlbumsRemoved     int64 `json:"albumsRemoved"`
	GenresRemoved     int64 `json:"genresRemoved"`
	PlaylistsAffected int64 `json:"playlistsAffected"`
	QueueItemsRemoved int64 `json:"queueItemsRemoved"`
}

// AddLibrary creates a new library from a directory path, emits a
// LibraryAdded event, and starts an asynchronous scan.
func (l *Library) AddLibrary(path string) (*sqlcgen.Library, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("%w: %s", errLibraryPathNotExist, path)
	}

	name := filepath.Base(path)

	lib, err := l.db.Queries.CreateLibrary(l.ctx, sqlcgen.CreateLibraryParams{
		Name: name,
		Path: path,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create library: %w", err)
	}

	// Claim orphaned tracks whose file_path falls under this library's
	// directory and still have the default library_id (0).  This handles
	// the case where tracks exist from a pre-multi-library schema.
	// SAFETY: Hand-crafted UPDATE because sqlc cannot express path-prefix
	// matching with LIKE and dynamic library_id in a single statement.
	// The WHERE clause is safe: library_id=0 targets only unclaimed rows,
	// and the path prefix is escaped by the query parameter.
	claimResult, claimErr := l.db.ExecContext(
		`UPDATE audio_files SET library_id = ? WHERE library_id = 0 AND file_path LIKE ? || '%'`,
		lib.ID, path+"/",
	)
	if claimErr != nil {
		l.logger.Warn("could not claim orphaned tracks",
			"libraryID", lib.ID, "path", path, "err", claimErr)
	} else if claimed, _ := claimResult.RowsAffected(); claimed > 0 {
		l.logger.Info("claimed orphaned tracks for new library",
			"libraryID", lib.ID, "claimed", claimed)
	}

	l.emit(events.LibraryAdded, lib)

	go func() {
		if scanErr := l.ScanLibrary(lib.ID); scanErr != nil {
			l.logger.Error("auto-scan after add failed",
				"libraryID", lib.ID,
				"err", scanErr,
			)
		}
	}()

	return &lib, nil
}

// LibraryPath resolves a library's root directory by id.
func (l *Library) LibraryPath(id int64) (string, error) {
	lib, err := l.db.ReadQueries.GetLibrary(l.ctx, id)
	if err != nil {
		return "", fmt.Errorf("could not get library %d: %w", id, err)
	}

	return lib.Path, nil
}

// RenameLibrary validates and updates a library's display name.
func (l *Library) RenameLibrary(id int64, newName string) error {
	newName = strings.TrimSpace(newName)

	if newName == "" {
		return errLibraryNameEmpty
	}

	if len(newName) > maxLibraryNameLength {
		return errLibraryNameTooLong
	}

	// Application-level uniqueness check (no schema migration needed).
	libs, err := l.db.Queries.GetAllLibraries(l.ctx)
	if err != nil {
		return fmt.Errorf("could not check existing names: %w", err)
	}

	for _, lib := range libs {
		if lib.ID != id && lib.Name == newName {
			return fmt.Errorf("%w: %q", errLibraryNameDuplicate, newName)
		}
	}

	if err := l.db.Queries.UpdateLibraryName(l.ctx, sqlcgen.UpdateLibraryNameParams{
		Name: newName,
		ID:   id,
	}); err != nil {
		return fmt.Errorf("could not rename library: %w", err)
	}

	l.emit(events.LibraryRenamed, map[string]any{
		"id":   id,
		"name": newName,
	})

	return nil
}

// GetRemovalImpact returns pre-removal counts for the confirmation
// dialog. All queries are read-only.
func (l *Library) GetRemovalImpact(libraryID int64) (*RemovalImpact, error) {
	// SAFETY: Hand-crafted SQL for track count. sqlc query CountAudioFilesByLibrary
	// exists but we inline the remaining two for consistency. Parameterized.
	trackCount, err := l.db.Queries.CountAudioFilesByLibrary(l.ctx, libraryID)
	if err != nil {
		return nil, fmt.Errorf("could not count tracks: %w", err)
	}

	// SAFETY: Hand-crafted SQL for playlists affected by library removal.
	// Multi-table JOIN with DISTINCT unsupported by sqlc. Parameterized.
	playlistsAffected, err := querySingleInt64(l.ctx, l.db,
		`SELECT COUNT(DISTINCT pt.playlist_id)
		 FROM playlist_tracks pt
		 JOIN audio_files af ON pt.audio_file_id = af.id
		 WHERE af.library_id = ?`,
		libraryID,
	)
	if err != nil {
		return nil, fmt.Errorf("could not count affected playlists: %w", err)
	}

	// SAFETY: Hand-crafted SQL for queue items affected by library removal.
	// Multi-table JOIN unsupported by sqlc. Parameterized.
	queueItemCount, err := querySingleInt64(l.ctx, l.db,
		`SELECT COUNT(*)
		 FROM queue_tracks qt
		 JOIN audio_files af ON qt.audio_file_id = af.id
		 WHERE af.library_id = ?`,
		libraryID,
	)
	if err != nil {
		return nil, fmt.Errorf("could not count queue items: %w", err)
	}

	return &RemovalImpact{
		TrackCount:        trackCount,
		PlaylistsAffected: playlistsAffected,
		QueueItemCount:    queueItemCount,
	}, nil
}

// RemoveLibrary atomically removes a library and all its data,
// performing orphan cleanup, phantom metadata conversion, FTS5
// rebuild, and queue compaction. Returns a summary of what was removed.
func (l *Library) RemoveLibrary(id int64) (*RemovalSummary, error) {
	// 1. Cancel active scan for this library and wait for it to stop.
	l.cancelLibraryScan(id)
	l.waitForScanIdle(id)

	// 2. Stop playback if the currently-playing track belongs to this library.
	if l.currentTrackBelongsToLibrary(id) {
		if l.removalHooks.StopPlayback != nil {
			l.removalHooks.StopPlayback()
		}
	}

	// 3. Pre-count for summary.
	impact, err := l.GetRemovalImpact(id)
	if err != nil {
		return nil, fmt.Errorf("could not get removal impact: %w", err)
	}

	// 4. Begin transaction.
	tx, err := l.db.BeginTx()
	if err != nil {
		return nil, fmt.Errorf("could not begin transaction: %w", err)
	}

	committed := false

	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// 5. Populate phantom metadata BEFORE deleting audio_files.
	// SAFETY: Hand-crafted UPDATE-FROM-SELECT for phantom metadata population.
	// Must run BEFORE DELETE FROM audio_files (which triggers SET NULL on
	// playlist_tracks.audio_file_id). Multi-table JOIN with subqueries
	// unsupported by sqlc. All values parameterized.
	if _, err := tx.ExecContext(l.ctx, `
		UPDATE playlist_tracks SET
			phantom_title = sub.title,
			phantom_artist = sub.artist,
			phantom_album = sub.album,
			phantom_duration_ms = sub.duration,
			phantom_genre = sub.genre,
			phantom_cover_art_path = sub.cover_art_path,
			phantom_file_path = sub.file_path
		FROM (
			SELECT
				pt.id AS pt_id,
				af.file_path AS file_path,
				COALESCE(r.name, '') AS title,
				COALESCE(ac.text, '') AS artist,
				COALESCE(rg.name, '') AS album,
				af.length_milliseconds AS duration,
				CAST(COALESCE(
					(SELECT GROUP_CONCAT(g.name, '||')
					 FROM recording_genres rg_sub
					 JOIN genres g ON rg_sub.genre_id = g.id
					 WHERE rg_sub.recording_id = r.id),
					''
				) AS TEXT) AS genre,
				COALESCE(ca.file_path, '') AS cover_art_path
			FROM playlist_tracks pt
			JOIN audio_files af ON pt.audio_file_id = af.id
			LEFT JOIN recordings r ON af.recording_id = r.id
			LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
			LEFT JOIN (
				SELECT recording_id, MIN(release_group_id) AS release_group_id
				FROM release_group_recordings
				GROUP BY recording_id
			) rgr ON r.id = rgr.recording_id
			LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
			LEFT JOIN cover_art ca ON rg.cover_art_id = ca.id
			WHERE af.library_id = ?
		) sub
		WHERE playlist_tracks.id = sub.pt_id`, id); err != nil {
		return nil, fmt.Errorf("could not populate phantom metadata: %w", err)
	}

	// 6. Delete audio_files (CASCADE deletes queue_tracks, SET NULL on playlist_tracks).
	// SAFETY: Hand-crafted DELETE for bulk removal by library_id.
	// sqlc DeleteAudioFile only handles single-row deletes. Parameterized.
	result, err := tx.ExecContext(l.ctx,
		`DELETE FROM audio_files WHERE library_id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("could not delete audio files: %w", err)
	}

	tracksDeleted, _ := result.RowsAffected()

	// 7. Delete orphaned recording_genres (must run BEFORE recordings
	// because recording_genres.recording_id references recordings.id).
	// SAFETY: Hand-crafted orphan cleanup SQL. Parameterless.
	if _, err := tx.ExecContext(l.ctx,
		`DELETE FROM recording_genres WHERE recording_id NOT IN (
			SELECT DISTINCT recording_id FROM audio_files
		)`); err != nil {
		return nil, fmt.Errorf("could not delete orphaned recording_genres: %w", err)
	}

	// 8. Delete orphaned release_group_recordings (must run BEFORE
	// recordings because release_group_recordings.recording_id
	// references recordings.id).
	// SAFETY: Hand-crafted orphan cleanup SQL. Parameterless.
	if _, err := tx.ExecContext(l.ctx,
		`DELETE FROM release_group_recordings WHERE recording_id NOT IN (
			SELECT DISTINCT recording_id FROM audio_files
		)`); err != nil {
		return nil, fmt.Errorf("could not delete orphaned release_group_recordings: %w", err)
	}

	// 9. Delete orphaned recordings (safe now that child tables are cleaned).
	// SAFETY: Hand-crafted orphan cleanup SQL. Reference-counting delete
	// with NOT IN subquery unsupported by sqlc. No user input.
	if _, err := tx.ExecContext(l.ctx,
		`DELETE FROM recordings WHERE id NOT IN (
			SELECT DISTINCT recording_id FROM audio_files
		)`); err != nil {
		return nil, fmt.Errorf("could not delete orphaned recordings: %w", err)
	}

	// 10. Delete orphaned release_groups.
	// SAFETY: Hand-crafted orphan cleanup SQL. Parameterless.
	result, err = tx.ExecContext(l.ctx,
		`DELETE FROM release_groups WHERE id NOT IN (
			SELECT DISTINCT release_group_id FROM release_group_recordings
		)`)
	if err != nil {
		return nil, fmt.Errorf("could not delete orphaned release_groups: %w", err)
	}

	albumsRemoved, _ := result.RowsAffected()

	// 11. Delete orphaned artist_credit_artists (must run BEFORE
	// artist_credit because artist_credit_artist.credit_id references
	// artist_credit.id).
	// SAFETY: Hand-crafted orphan cleanup SQL. Parameterless.
	if _, err := tx.ExecContext(l.ctx,
		`DELETE FROM artist_credit_artist WHERE credit_id NOT IN (
			SELECT DISTINCT artist_credit_id FROM recordings
		) AND credit_id NOT IN (
			SELECT DISTINCT album_artist_credit_id FROM release_groups
			WHERE album_artist_credit_id IS NOT NULL
		)`); err != nil {
		return nil, fmt.Errorf("could not delete orphaned artist_credit_artists: %w", err)
	}

	// 12. Delete orphaned artist_credits (safe now that child table is cleaned).
	// SAFETY: Hand-crafted orphan cleanup SQL. Dual-FK reference counting
	// (recordings.artist_credit_id + release_groups.album_artist_credit_id)
	// unsupported by sqlc. Parameterless.
	if _, err := tx.ExecContext(l.ctx,
		`DELETE FROM artist_credit WHERE id NOT IN (
			SELECT DISTINCT artist_credit_id FROM recordings
		) AND id NOT IN (
			SELECT DISTINCT album_artist_credit_id FROM release_groups
			WHERE album_artist_credit_id IS NOT NULL
		)`); err != nil {
		return nil, fmt.Errorf("could not delete orphaned artist_credits: %w", err)
	}

	// 13. Delete orphaned artists.
	// SAFETY: Hand-crafted orphan cleanup SQL. Parameterless.
	result, err = tx.ExecContext(l.ctx,
		`DELETE FROM artists WHERE id NOT IN (
			SELECT DISTINCT artist_id FROM artist_credit_artist
		)`)
	if err != nil {
		return nil, fmt.Errorf("could not delete orphaned artists: %w", err)
	}

	artistsRemoved, _ := result.RowsAffected()

	// 14. Delete orphaned genres.
	// SAFETY: Hand-crafted orphan cleanup SQL. Parameterless.
	result, err = tx.ExecContext(l.ctx,
		`DELETE FROM genres WHERE id NOT IN (
			SELECT DISTINCT genre_id FROM recording_genres
		)`)
	if err != nil {
		return nil, fmt.Errorf("could not delete orphaned genres: %w", err)
	}

	genresRemoved, _ := result.RowsAffected()

	// 15. Collect orphaned cover_art file paths for post-commit cleanup.
	// SAFETY: Hand-crafted SELECT for orphaned cover art identification.
	// Parameterless.
	rows, err := tx.QueryContext(l.ctx,
		`SELECT file_path FROM cover_art WHERE id NOT IN (
			SELECT DISTINCT cover_art_id FROM release_groups
			WHERE cover_art_id IS NOT NULL
		)`)
	if err != nil {
		return nil, fmt.Errorf("could not query orphaned cover art: %w", err)
	}

	var orphanedCoverArtPaths []string

	for rows.Next() {
		var filePath string
		if err := rows.Scan(&filePath); err != nil {
			l.logger.Warn("could not scan cover art path", "err", err)

			continue
		}

		orphanedCoverArtPaths = append(orphanedCoverArtPaths, filePath)
	}

	if err := rows.Close(); err != nil {
		l.logger.Warn("could not close cover art rows", "err", err)
	}

	// 16. Delete orphaned cover_art rows.
	// SAFETY: Hand-crafted orphan cleanup SQL. Parameterless.
	if _, err := tx.ExecContext(l.ctx,
		`DELETE FROM cover_art WHERE id NOT IN (
			SELECT DISTINCT cover_art_id FROM release_groups
			WHERE cover_art_id IS NOT NULL
		)`); err != nil {
		return nil, fmt.Errorf("could not delete orphaned cover_art: %w", err)
	}

	// 17. Delete the library's tagging queue.  tagging_items holds a
	// FOREIGN KEY to libraries with no ON DELETE clause, so leaving these
	// rows behind makes the DELETE below fail the whole transaction and
	// the library becomes unremovable.  tagging_candidates is tied to
	// tagging_items by ON DELETE CASCADE and goes with it.
	// SAFETY: Hand-crafted DELETE — sqlc has no query for this. Parameterized.
	if _, err := tx.ExecContext(l.ctx,
		`DELETE FROM tagging_items WHERE library_id = ?`, id); err != nil {
		return nil, fmt.Errorf("could not delete tagging items: %w", err)
	}

	// 18. Delete library row.
	// SAFETY: Hand-crafted DELETE matching sqlc DeleteLibrary but within
	// the same transaction. Parameterized.
	if _, err := tx.ExecContext(l.ctx,
		`DELETE FROM libraries WHERE id = ?`, id); err != nil {
		return nil, fmt.Errorf("could not delete library: %w", err)
	}

	// 19. Commit transaction.
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("could not commit removal transaction: %w", err)
	}

	committed = true

	// 20. FTS5 rebuild skipped — contentless FTS5 (content='') cannot
	// delete individual rows, but stale entries are harmless: search
	// queries JOIN against track_metadata which filters out deleted
	// rows. The index is rebuilt on the next full rescan. Skipping
	// avoids a costly full re-index of all remaining tracks (~10s for
	// 25K tracks).

	// 21. Post-commit: Delete orphaned cover art files and their sized
	// variants.  Only the original is stored in cover_art.file_path; the
	// _sm/_md/_lg thumbnails are derived filenames beside it, so they
	// have to be removed by name or they accumulate forever.
	for _, coverPath := range orphanedCoverArtPaths {
		for _, path := range CoverArtFileSet(coverPath) {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				l.logger.Warn("could not remove orphaned cover art file",
					"path", path,
					"err", err,
				)
			}
		}
	}

	// 22. Post-commit: Compact queue.
	if l.removalHooks.CompactQueue != nil {
		l.removalHooks.CompactQueue()
	}

	// 23. Post-commit: invalidate library-sync markers so the gated
	// index/lyric re-sync runs on the next launch.
	if l.removalHooks.PostRemove != nil {
		l.removalHooks.PostRemove()
	}

	summary := &RemovalSummary{
		TracksDeleted:     tracksDeleted,
		ArtistsRemoved:    artistsRemoved,
		AlbumsRemoved:     albumsRemoved,
		GenresRemoved:     genresRemoved,
		PlaylistsAffected: impact.PlaylistsAffected,
		QueueItemsRemoved: impact.QueueItemCount,
	}

	// 22. Emit events.
	l.emit(events.LibraryRemoved, map[string]any{
		"id":      id,
		"summary": summary,
	})

	return summary, nil
}

// waitForScanIdle polls until the specified library is no longer the
// active scan target. Called after cancelLibraryScan to ensure the
// scan goroutine has finished before proceeding with removal.
func (l *Library) waitForScanIdle(id int64) {
	for range 100 { // up to ~5 seconds
		l.mu.Lock()
		active := l.currentScanLibraryID == id
		l.mu.Unlock()

		if !active {
			return
		}

		time.Sleep(50 * time.Millisecond)
	}

	l.logger.Warn("timed out waiting for scan to stop",
		"libraryID", id)
}

// cancelLibraryScan cancels an active scan for the specified library
// and removes it from the scan queue.
func (l *Library) cancelLibraryScan(id int64) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// If this library is currently scanning, cancel it.
	if l.currentScanLibraryID == id {
		if l.scanCancel != nil {
			l.scanCancel()
		}
	}

	// Remove from the scan queue if queued.
	filtered := l.scanQueue[:0]

	for _, entry := range l.scanQueue {
		if entry.libraryID != id {
			filtered = append(filtered, entry)
		}
	}

	l.scanQueue = filtered
}

// currentTrackBelongsToLibrary checks whether the currently-playing
// queue track belongs to the specified library.
func (l *Library) currentTrackBelongsToLibrary(libraryID int64) bool {
	// SAFETY: Hand-crafted SQL to check if the current queue track belongs
	// to the library being removed. Multi-table JOIN with subquery
	// unsupported by sqlc. Parameterized.
	count, err := querySingleInt64(l.ctx, l.db,
		`SELECT COUNT(*) FROM audio_files af
		 JOIN queue_tracks qt ON qt.audio_file_id = af.id
		 WHERE af.library_id = ?
		 AND qt.position = (
			SELECT current_position FROM queue LIMIT 1
		 )`,
		libraryID,
	)
	if err != nil {
		l.logger.Warn("could not check if current track belongs to library",
			"libraryID", libraryID,
			"err", err,
		)

		return false
	}

	return count > 0
}

// querySingleInt64 executes a query that returns a single integer value.
func querySingleInt64(
	_ context.Context,
	db *database.DB,
	query string,
	args ...any,
) (int64, error) {
	rows, err := db.QueryContext(query, args...)
	if err != nil {
		return 0, err
	}

	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return 0, errQueryNoRows
	}

	var val int64
	if err := rows.Scan(&val); err != nil {
		return 0, err
	}

	return val, rows.Err()
}
