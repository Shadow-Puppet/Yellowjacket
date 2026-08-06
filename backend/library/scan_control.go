package library

import (
	"context"

	"yellowjacket/backend/events"
	"yellowjacket/backend/jobs"
)

// CancelScan cancels an in-progress scan. Returns immediately;
// scan goroutines stop at their next checkpoint.
//
// Deprecated: Use CancelCurrentScan or CancelAllScans for
// queue-aware cancellation.
func (l *Library) CancelScan() {
	l.mu.Lock()
	cancel := l.scanCancel
	l.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// PauseScan pauses an in-progress scan. Workers block at their
// next pause checkpoint until ResumeScan is called.
func (l *Library) PauseScan() {
	l.mu.Lock()

	if !l.scanActive || l.scanPaused {
		l.mu.Unlock()

		return
	}

	l.scanPaused = true
	l.scanPauseCh = make(chan struct{})
	pausedID := l.currentScanLibraryID
	reg := l.jobs
	l.mu.Unlock()

	l.emit(events.LibraryScanPaused)

	// Confirm the pause on the job — the registry moved it to "pausing"
	// when the request came in.  Writing the durable pause record is a
	// side effect of reaching StatePaused, so a scan paused now comes
	// back paused after a restart.  Done after releasing l.mu: this
	// writes to the database, and workers take l.mu on every pause
	// checkpoint.
	if reg == nil {
		return
	}

	if h := reg.Get(scanJobID(pausedID)); h != nil {
		h.SetState(jobs.StatePaused)
		h.SetPhase("Paused")
		h.Logf(jobs.LevelInfo, "Scan paused")
	}
}

// ResumeScan unblocks a paused scan.
func (l *Library) ResumeScan() {
	l.mu.Lock()

	if !l.scanPaused {
		l.mu.Unlock()

		return
	}

	l.scanPaused = false
	close(l.scanPauseCh) // unblocks all waiting workers
	resumedID := l.currentScanLibraryID
	reg := l.jobs
	l.mu.Unlock()

	l.emit(events.LibraryScanResumed)

	// Clears the durable pause record as a side effect of leaving
	// StatePaused.
	if reg == nil {
		return
	}

	if h := reg.Get(scanJobID(resumedID)); h != nil {
		h.SetState(jobs.StateRunning)
		h.Logf(jobs.LevelInfo, "Scan resumed")
	}
}

// IsScanActive returns whether a scan is currently running.
func (l *Library) IsScanActive() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.scanActive
}

// IsScanPaused returns whether the scan is currently paused.
func (l *Library) IsScanPaused() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.scanPaused
}

// waitIfPaused blocks the calling goroutine if the scan is paused.
// Returns ctx.Err() if the context is cancelled while waiting.
func (l *Library) waitIfPaused(ctx context.Context) error {
	l.mu.Lock()
	ch := l.scanPauseCh
	paused := l.scanPaused
	l.mu.Unlock()

	if !paused || ch == nil {
		return nil
	}

	select {
	case <-ch: // closed = unpaused
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
