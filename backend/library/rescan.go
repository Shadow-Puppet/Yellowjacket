package library

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"yellowjacket/backend/events"
	"yellowjacket/backend/system"
)

// FullRescan clears the queue and player, wipes all library data
// (database records and cover art files), and performs a fresh
// scan from scratch.
func (l *Library) FullRescan() error {
	l.logger.Info("beginning full library rescan")

	runtime.EventsEmit(l.ctx, events.LibraryScanStarted)

	// Stop playback and clear the queue before wiping data so
	// the player is not referencing now-deleted tracks.
	if l.queue != nil {
		l.queue.Clear()
	}

	if err := l.clearLibraryData(); err != nil {
		return fmt.Errorf("could not clear library data: %w", err)
	}

	return l.Scan()
}

// clearLibraryData removes all library-related records from the
// database and deletes all cover art files from disk.  The deletes
// are executed in FK-safe order within a single transaction.
func (l *Library) clearLibraryData() error {
	l.logger.Info("clearing all library data")

	if err := l.clearLibraryTables(); err != nil {
		return err
	}

	if err := l.clearCoverArtFiles(); err != nil {
		return err
	}

	l.logger.Info("library data cleared successfully")

	return nil
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

	if err := tx.Commit(); err != nil {
		return fmt.Errorf(
			"could not commit library clear transaction: %w", err,
		)
	}

	l.logger.Info("all library tables cleared")

	return nil
}

// clearCoverArtFiles removes all files from the covers directory.
func (l *Library) clearCoverArtFiles() error {
	dataDir, err := system.GetUserDataDirPath()
	if err != nil {
		return fmt.Errorf(
			"could not get user data directory: %w", err,
		)
	}

	coverDir := filepath.Join(dataDir, "covers")

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
