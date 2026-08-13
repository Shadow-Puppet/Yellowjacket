package queue

import (
	"testing"

	"yellowjacket/backend/events"
)

// A finished track used to emit TrackMetadataChanged — the event that
// means "the tags on disk were rewritten" — which the frontend answers
// by discarding its entire library cache and refetching tracks, albums,
// artists and genres.  Measured on a 50 000-track library that is
// ~37 MB across the IPC and ~0.8 s of blocked main thread per song, and
// it cleared the user's track selection every time (audit perf.C1/C2).
//
// So the assertion that matters is not only that the new event fires:
// it is that the old one *stops*.  A change that added
// TrackPlayCountChanged and left TrackMetadataChanged in place would
// look right in the payload and fix nothing.
func TestRecordPlay_EmitsPlayCountNotMetadataChanged(t *testing.T) {
	t.Parallel()

	q, db, rec := setupRecordedQueue(t)
	paths := seedAudioFiles(t, db, 2)

	q.SetQueue(paths, 0, false, Source{})
	rec.Reset()

	q.recordPlay(1)

	if n := rec.Count(events.TrackMetadataChanged); n != 0 {
		t.Errorf(
			"recording a play emitted TrackMetadataChanged %d time(s); "+
				"that event invalidates the whole library cache",
			n,
		)
	}

	ev, ok := rec.Last(events.TrackPlayCountChanged)
	if !ok {
		t.Fatalf(
			"no TrackPlayCountChanged emitted; got %v", rec.Names(),
		)
	}

	payload, ok := ev.Payload().(map[string]any)
	if !ok {
		t.Fatalf("payload is %T, want map[string]any", ev.Payload())
	}

	// The point of the payload is that it is enough to patch one track
	// in place, so a consumer never needs to refetch anything.  A
	// missing field here means a consumer has to.
	for _, key := range []string{
		"audioFileId", "filePath", "playCount", "lastPlayed",
	} {
		if _, ok := payload[key]; !ok {
			t.Errorf("payload is missing %q: %v", key, payload)
		}
	}

	if got := payload["filePath"]; got != paths[0] {
		t.Errorf("filePath: got %v, want %v", got, paths[0])
	}

	if got, want := payload["playCount"], int64(1); got != want {
		t.Errorf("playCount: got %v (%T), want %v", got, got, want)
	}
}

// The count comes from the database rather than from a counter the
// event handler keeps, so two plays report 1 then 2 — and a frontend
// that renders the payload cannot drift from the stored value.
func TestRecordPlay_ReportsTheStoredCount(t *testing.T) {
	t.Parallel()

	q, db, rec := setupRecordedQueue(t)
	paths := seedAudioFiles(t, db, 1)

	q.SetQueue(paths, 0, false, Source{})
	rec.Reset()

	q.recordPlay(1)
	q.recordPlay(1)

	got := make([]any, 0, 2)

	for _, ev := range rec.Named(events.TrackPlayCountChanged) {
		payload, ok := ev.Payload().(map[string]any)
		if !ok {
			t.Fatalf("payload is %T, want map[string]any", ev.Payload())
		}

		got = append(got, payload["playCount"])
	}

	if len(got) != 2 {
		t.Fatalf("got %d events, want 2", len(got))
	}

	if got[0] != int64(1) || got[1] != int64(2) {
		t.Errorf("play counts: got %v, want [1 2]", got)
	}
}
