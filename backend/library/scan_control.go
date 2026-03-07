package library

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"yellowjacket/backend/events"
)

// CancelScan cancels an in-progress scan. Returns immediately;
// scan goroutines stop at their next checkpoint.
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
	defer l.mu.Unlock()

	if !l.scanActive || l.scanPaused {
		return
	}

	l.scanPaused = true
	l.scanPauseCh = make(chan struct{})

	runtime.EventsEmit(l.ctx, events.LibraryScanPaused)
}

// ResumeScan unblocks a paused scan.
func (l *Library) ResumeScan() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.scanPaused {
		return
	}

	l.scanPaused = false
	close(l.scanPauseCh) // unblocks all waiting workers

	runtime.EventsEmit(l.ctx, events.LibraryScanResumed)
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
