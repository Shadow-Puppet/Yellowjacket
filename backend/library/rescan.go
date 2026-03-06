package library

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"yellowjacket/backend/coverart"
)

// FullRescan clears the queue and player, wipes all library data
// (database records and cover art files), and performs a fresh
// scan from scratch.  The returned ScanMetrics includes timing
// for the clear phases in addition to the normal scan metrics.
func (l *Library) FullRescan() (*ScanMetrics, error) {
	l.logger.Info("beginning full library rescan")

	// Run the pre-clear hook (e.g. clear queue / stop playback)
	// before wiping data so the player is not referencing
	// now-deleted tracks.
	clearQueueStart := time.Now()

	if l.rescanHooks.PreClear != nil {
		l.rescanHooks.PreClear()
	}

	clearQueueDur := time.Since(clearQueueStart)

	// Clear all library data (DB + cover art files).
	clearDBStart := time.Now()

	if err := l.clearLibraryTables(); err != nil {
		return nil, fmt.Errorf(
			"could not clear library tables: %w", err,
		)
	}

	clearDBDur := time.Since(clearDBStart)

	clearFilesStart := time.Now()

	if err := l.clearCoverArtFiles(); err != nil {
		return nil, fmt.Errorf(
			"could not clear cover art files: %w", err,
		)
	}

	clearFilesDur := time.Since(clearFilesStart)

	l.logger.Info("library data cleared successfully")

	// Run the full scan and merge clear-phase times into
	// the metrics it returns.
	metrics, err := l.Scan()
	if metrics != nil {
		metrics.ClearQueue = clearQueueDur
		metrics.ClearDatabase = clearDBDur
		metrics.ClearCoverFiles = clearFilesDur

		// Include clear-phase durations in the total so the
		// displayed value reflects true wall-clock time.
		metrics.Total += clearQueueDur +
			clearDBDur + clearFilesDur
	}

	// Run the post-scan hook (e.g. restore playlists from M3U8
	// files) now that audio_files are populated again.
	if l.rescanHooks.PostScan != nil {
		l.rescanHooks.PostScan()
	}

	return metrics, err
}

// clearLibraryTables deletes all library-related rows in FK-safe
// order within a single transaction.
func (l *Library) clearLibraryTables() error {
	tx, err := l.db.BeginTx()
	if err != nil {
		return fmt.Errorf("could not begin transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback()
	}()

	txq := l.db.Queries.WithTx(tx)

	// Phase 1: leaf tables (nothing references these).
	if err := txq.ClearQueueTracks(l.ctx); err != nil {
		return fmt.Errorf("could not clear queue tracks: %w", err)
	}

	if err := txq.DeleteAllPlaylistTracks(l.ctx); err != nil {
		return fmt.Errorf(
			"could not clear playlist tracks: %w", err,
		)
	}

	if err := txq.DeleteAllRecordingGenres(l.ctx); err != nil {
		return fmt.Errorf(
			"could not clear recording genres: %w", err,
		)
	}

	if err := txq.DeleteAllReleaseGroupRecordings(l.ctx); err != nil {
		return fmt.Errorf(
			"could not clear release group recordings: %w", err,
		)
	}

	if err := txq.DeleteAllArtistCreditArtists(l.ctx); err != nil {
		return fmt.Errorf(
			"could not clear artist credit artists: %w", err,
		)
	}

	// Phase 2: mid-level tables.
	if err := txq.DeleteAllAudioFiles(l.ctx); err != nil {
		return fmt.Errorf(
			"could not clear audio files: %w", err,
		)
	}

	if err := txq.DeleteAllReleaseGroups(l.ctx); err != nil {
		return fmt.Errorf(
			"could not clear release groups: %w", err,
		)
	}

	if err := txq.DeleteAllRecordings(l.ctx); err != nil {
		return fmt.Errorf(
			"could not clear recordings: %w", err,
		)
	}

	// Phase 3: root tables.
	if err := txq.DeleteAllCoverArt(l.ctx); err != nil {
		return fmt.Errorf(
			"could not clear cover art: %w", err,
		)
	}

	if err := txq.DeleteAllArtistCredits(l.ctx); err != nil {
		return fmt.Errorf(
			"could not clear artist credits: %w", err,
		)
	}

	if err := txq.DeleteAllArtists(l.ctx); err != nil {
		return fmt.Errorf(
			"could not clear artists: %w", err,
		)
	}

	if err := txq.DeleteAllGenres(l.ctx); err != nil {
		return fmt.Errorf(
			"could not clear genres: %w", err,
		)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf(
			"could not commit library clear transaction: %w", err,
		)
	}

	// Clear FTS5 search index AFTER the transaction.
	// ClearSearchIndex drops and recreates the contentless FTS5
	// virtual table, which cannot run inside a transaction.
	if err := l.db.ClearSearchIndex(); err != nil {
		return fmt.Errorf(
			"could not clear search index: %w", err,
		)
	}

	l.logger.Info("all library tables cleared")

	return nil
}

// clearCoverArtFiles removes all files from the covers directory.
func (l *Library) clearCoverArtFiles() error {
	coverDir, err := coverart.CoversDir()
	if err != nil {
		return fmt.Errorf(
			"could not resolve covers directory: %w", err,
		)
	}

	entries, err := os.ReadDir(coverDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return fmt.Errorf(
			"could not read covers directory: %w", err,
		)
	}

	var removed int

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		path := filepath.Join(coverDir, entry.Name())
		if err := os.Remove(path); err != nil {
			l.logger.Warn(
				"could not remove cover art file",
				"path", path, "err", err,
			)

			continue
		}

		removed++
	}

	l.logger.Info("cover art files removed", "count", removed)

	return nil
}
