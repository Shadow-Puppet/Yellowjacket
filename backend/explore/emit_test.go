package explore

import (
	"context"
	"log/slog"
	"testing"

	"yellowjacket/backend/database"
	"yellowjacket/backend/events"
)

// The index status used to be pushed on a 3 s ticker for the life of
// the process, with a byte-identical payload once the index was ready.
// The frontend's handler assigns it to a @state field, so every tick
// re-rendered the whole settings page — a cached view that never
// unmounts — saying nothing (`perf.M6` / `H-14`).
//
// The ticker is gone and emitStatus suppresses an unchanged payload, so
// what these cover is the pair of properties that replaced it: a change
// still gets through, and a non-change does not.

// setupRecordedIndex builds a SearchIndex with an event sink installed,
// so what the frontend would receive is assertable in-process.
func setupRecordedIndex(t *testing.T) (*SearchIndex, *events.Recorder) {
	t.Helper()

	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, slog.Default())

	rec := events.NewRecorder()
	si.SetContext(events.WithSink(context.Background(), rec))

	return si, rec
}

func TestEmitStatus_SuppressesAnUnchangedPayload(t *testing.T) {
	si, rec := setupRecordedIndex(t)

	// SetContext emits once via refreshStatusCounts; everything after
	// this describes the same state.
	rec.Reset()

	for range 5 {
		si.emitStatus()
	}

	if got := rec.Count(events.IndexStatusChanged); got != 0 {
		t.Fatalf(
			"emitted %d IndexStatusChanged for an unchanged status, want 0",
			got,
		)
	}
}

func TestEmitStatus_EmitsOnChange(t *testing.T) {
	si, rec := setupRecordedIndex(t)
	rec.Reset()

	si.setTierStatus("artists", "running", 100, 10)

	if got := rec.Count(events.IndexStatusChanged); got != 1 {
		t.Fatalf("emitted %d IndexStatusChanged for a new tier, want 1", got)
	}

	// Progress within the tier is a change too — this is what a build
	// reports, and dropping it would freeze the progress bar.
	si.setTierStatus("artists", "running", 100, 20)

	if got := rec.Count(events.IndexStatusChanged); got != 2 {
		t.Fatalf("emitted %d after progress moved, want 2", got)
	}

	// The same call again is not.
	si.setTierStatus("artists", "running", 100, 20)

	if got := rec.Count(events.IndexStatusChanged); got != 2 {
		t.Fatalf("emitted %d after a repeated status, want 2", got)
	}
}

// The status carries a slice, so a snapshot that aliases it would
// compare equal to a later mutation of the same backing array and the
// change would never be emitted.
func TestEmitStatus_SnapshotDoesNotAliasTiers(t *testing.T) {
	si, rec := setupRecordedIndex(t)

	si.setTierStatus("artists", "running", 100, 10)
	rec.Reset()

	si.setTierStatus("artists", "complete", 100, 100)

	if got := rec.Count(events.IndexStatusChanged); got != 1 {
		t.Fatalf(
			"emitted %d when a tier completed in place, want 1", got,
		)
	}

	status, ok := rec.Last(events.IndexStatusChanged)
	if !ok {
		t.Fatal("no IndexStatusChanged recorded")
	}

	payload, ok := status.Payload().(IndexStatus)
	if !ok {
		t.Fatalf("payload is %T, want explore.IndexStatus", status.Payload())
	}

	if len(payload.Tiers) != 1 || payload.Tiers[0].State != "complete" {
		t.Fatalf("payload tiers = %+v, want one complete tier", payload.Tiers)
	}
}

// `Building` and `Ready` are derived inside emitStatus from fields the
// dedupe never sees directly, so a transition in either has to survive
// it. A build ending is the case that matters: syncIndexJob only
// resolves the job in the registry on a sync reporting Building false,
// and suppressing that left the header badge saying "Building search
// index" over an index the settings page called ready.
func TestEmitStatus_BuildingTransitionsAreNotSuppressed(t *testing.T) {
	si, rec := setupRecordedIndex(t)

	si.mu.Lock()
	_, si.cancel = context.WithCancel(context.Background())
	si.mu.Unlock()

	si.emitStatus()
	rec.Reset()

	// Nothing else changed, so this one is noise.
	si.emitStatus()

	if got := rec.Count(events.IndexStatusChanged); got != 0 {
		t.Fatalf("emitted %d while still building unchanged, want 0", got)
	}

	si.mu.Lock()
	si.cancel = nil
	si.mu.Unlock()

	si.emitStatus()

	ev, ok := rec.Last(events.IndexStatusChanged)
	if !ok {
		t.Fatalf("a build ending emitted nothing; got %v", rec.Names())
	}

	payload, ok := ev.Payload().(IndexStatus)
	if !ok {
		t.Fatalf("payload is %T, want explore.IndexStatus", ev.Payload())
	}

	if payload.Building {
		t.Fatal("payload still reports building after the build ended")
	}
}

// Becoming ready used to be the one status mutation with no emit behind
// it; the ticker carried it, so removing the ticker without an explicit
// emit would have left the settings page reading "not ready" forever
// over a fully built index.
func TestEmitStatus_ReadyIsAChange(t *testing.T) {
	si, rec := setupRecordedIndex(t)
	rec.Reset()

	si.mu.Lock()
	si.ready = true
	si.mu.Unlock()

	si.emitStatus()

	ev, ok := rec.Last(events.IndexStatusChanged)
	if !ok {
		t.Fatalf("becoming ready emitted nothing; got %v", rec.Names())
	}

	payload, ok := ev.Payload().(IndexStatus)
	if !ok {
		t.Fatalf("payload is %T, want explore.IndexStatus", ev.Payload())
	}

	if !payload.Ready {
		t.Fatal("payload reports not ready after si.ready was set")
	}
}
