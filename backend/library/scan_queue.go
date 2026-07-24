package library

import (
	"fmt"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"yellowjacket/backend/events"
)

// scanQueueEntry holds the metadata needed to scan a single library.
type scanQueueEntry struct {
	libraryID   int64
	libraryName string
	libraryPath string
}

// ScanLibrary queues a scan for the library with the given database ID.
// If no scan is active the library is scanned immediately; otherwise it
// is appended to the queue. Duplicate requests (same library already
// scanning or already queued) are silently ignored.
func (l *Library) ScanLibrary(id int64) error {
	lib, err := l.db.Queries.GetLibrary(l.ctx, id)
	if err != nil {
		return fmt.Errorf("could not get library %d: %w", id, err)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Silent dedup: already scanning this library.
	if l.currentScanLibraryID == id {
		return nil
	}

	// Silent dedup: already queued.
	for _, entry := range l.scanQueue {
		if entry.libraryID == id {
			return nil
		}
	}

	entry := scanQueueEntry{
		libraryID:   lib.ID,
		libraryName: lib.Name,
		libraryPath: lib.Path,
	}

	if !l.scanActive {
		l.scanActive = true
		l.currentScanLibraryID = entry.libraryID
		l.currentScanLibraryName = entry.libraryName

		go l.startScan(entry)

		return nil
	}

	// A scan is already running — queue this library.
	l.scanQueue = append(l.scanQueue, entry)

	runtime.EventsEmit(l.ctx, events.LibraryScanQueued, map[string]any{
		"libraryId":   lib.ID,
		"libraryName": lib.Name,
		"queueLength": len(l.scanQueue),
	})

	return nil
}

// ScanAllLibraries queries all libraries from the database and queues
// each one for scanning. Existing dedup logic ensures no duplicates.
func (l *Library) ScanAllLibraries() error {
	libs, err := l.db.Queries.GetAllLibraries(l.ctx)
	if err != nil {
		return fmt.Errorf("could not get all libraries: %w", err)
	}

	for _, lib := range libs {
		if err := l.ScanLibrary(lib.ID); err != nil {
			l.logger.Warn(
				"could not queue library for scan",
				"libraryID", lib.ID,
				"libraryName", lib.Name,
				"err", err,
			)
		}
	}

	return nil
}

// SoftScanAllLibraries performs a lightweight launch-time scan.
// First it claims any orphaned tracks (library_id=0) left over from
// the pre-multi-library schema. Then for each library it compares
// the number of audio files on disk against the track count in the
// database. Only libraries where the counts differ (files added or
// removed since last scan) are queued for a full scan. Libraries
// that are unchanged are silently skipped — no progress bar, no
// scan events.
func (l *Library) SoftScanAllLibraries() error {
	libs, err := l.db.Queries.GetAllLibraries(l.ctx)
	if err != nil {
		return fmt.Errorf("could not get all libraries: %w", err)
	}

	// Claim orphaned tracks from the pre-multi-library schema.
	// Tracks with library_id=0 exist from before the migration and
	// need to be assigned to the library whose path matches.
	// SAFETY: Hand-crafted UPDATE — path-prefix LIKE matching with
	// dynamic library_id unsupported by sqlc. No user input.
	for _, lib := range libs {
		result, claimErr := l.db.ExecContext(
			`UPDATE audio_files SET library_id = ?`+
				` WHERE library_id = 0`+
				` AND file_path LIKE ? || '%'`,
			lib.ID, lib.Path+"/",
		)
		if claimErr != nil {
			l.logger.Warn("soft scan: could not claim orphaned tracks",
				"libraryID", lib.ID, "err", claimErr)
		} else if claimed, _ := result.RowsAffected(); claimed > 0 {
			l.logger.Info("soft scan: claimed orphaned tracks",
				"libraryID", lib.ID, "libraryName", lib.Name,
				"claimed", claimed)
		}
	}

	for _, lib := range libs {
		dbCount, countErr := l.db.Queries.CountAudioFilesByLibrary(
			l.ctx, lib.ID,
		)
		if countErr != nil {
			l.logger.Warn(
				"soft scan: could not count DB tracks, queueing full scan",
				"libraryID", lib.ID,
				"libraryName", lib.Name,
				"err", countErr,
			)

			_ = l.ScanLibrary(lib.ID)

			continue
		}

		diskCount := countAudioFiles(lib.Path)

		if diskCount == dbCount {
			l.logger.Info(
				"soft scan: library unchanged, skipping",
				"libraryID", lib.ID,
				"libraryName", lib.Name,
				"tracks", dbCount,
			)

			continue
		}

		l.logger.Info(
			"soft scan: file count mismatch, queueing scan",
			"libraryID", lib.ID,
			"libraryName", lib.Name,
			"diskFiles", diskCount,
			"dbTracks", dbCount,
		)

		if err := l.ScanLibrary(lib.ID); err != nil {
			l.logger.Warn(
				"soft scan: could not queue library",
				"libraryID", lib.ID,
				"libraryName", lib.Name,
				"err", err,
			)
		}
	}

	return nil
}

// CancelCurrentScan cancels only the currently scanning library.
// The next queued library (if any) starts automatically when the
// current scan's goroutine completes.
func (l *Library) CancelCurrentScan() {
	l.mu.Lock()
	cancel := l.scanCancel
	l.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// CancelAllScans cancels the current scan and clears the entire
// queue so no further libraries are scanned.
func (l *Library) CancelAllScans() {
	l.mu.Lock()
	l.scanQueue = nil
	cancel := l.scanCancel
	l.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// GetScanQueueLength returns the number of libraries waiting in the
// scan queue (excludes the currently scanning library).
func (l *Library) GetScanQueueLength() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return len(l.scanQueue)
}

// QueuedLibraryNames returns the display names of libraries waiting
// in the scan queue, in FIFO order.
func (l *Library) QueuedLibraryNames() []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	names := make([]string, len(l.scanQueue))
	for i, entry := range l.scanQueue {
		names[i] = entry.libraryName
	}

	return names
}

// startScan runs the scan for a single library entry and then drains
// the queue. It is always called in a new goroutine.
func (l *Library) startScan(entry scanQueueEntry) {
	l.scanInternal(entry.libraryID, entry.libraryName, entry.libraryPath)
	l.drainQueue()
}

// drainQueue is called after each scan completes. If the queue is
// non-empty the next entry is popped and scanned; otherwise the
// scan pipeline is marked idle.
func (l *Library) drainQueue() {
	l.mu.Lock()

	if len(l.scanQueue) > 0 {
		next := l.scanQueue[0]
		l.scanQueue = l.scanQueue[1:]
		l.currentScanLibraryID = next.libraryID
		l.currentScanLibraryName = next.libraryName
		l.mu.Unlock()

		go l.startScan(next)

		return
	}

	l.currentScanLibraryID = 0
	l.currentScanLibraryName = ""
	l.scanActive = false
	hooks := l.scanHooks
	l.mu.Unlock()

	runtime.EventsEmit(l.ctx, events.LibraryScanQueueDrained)

	if hooks.OnAllScansComplete != nil {
		hooks.OnAllScansComplete()
	}
}
