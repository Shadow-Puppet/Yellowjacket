package player

import (
	"log/slog"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"yellowjacket/backend/events"
	"yellowjacket/internal/testfixtures"
)

// fixtureSampleRate is what cmd/gentestdata writes (audio.go).  It is
// deliberately not the speaker rate, which is what lets these tests
// tell the decoder's format from the player's default.
const fixtureSampleRate = 22050

// newTestPlayer is a player with a context and no database, so the
// track-metadata lookup cannot succeed.
func newTestPlayer(t *testing.T) *Player {
	t.Helper()

	p := NewPlayer(slog.Default(), nil)
	rec := events.NewRecorder()

	_ = p.ServiceStartup(
		events.WithSink(t.Context(), rec),
		application.ServiceOptions{},
	)

	return p
}

// loadFileLocked needs no speaker: it decodes, builds the chain and
// registers it paused. speaker.Play on an uninitialised device is
// what the integration guard elsewhere is about, so these assert on
// the state the load computed rather than on playback.

// p.format used to be assigned once, in the constructor, to the
// *speaker's* rate -- so it claimed 44.1 kHz for every file ever
// loaded. Play()'s replay-after-finish path resamples from it, so a
// finished track played again was resampled from a rate the decoder
// never produced: audibly the wrong speed and pitch, and wrong
// length and position arithmetic with it.
//
// The fixtures are 22050 Hz, which is exactly the point -- any of
// them disagrees with the speaker rate.
func TestLoadRecordsTheDecodersOwnFormat(t *testing.T) {
	m := testfixtures.Load(t)
	path := m.Case(t, testfixtures.CaseCoverDedup)[0]

	p := newTestPlayer(t)

	if got := p.format.SampleRate; got != speakerSampleRate {
		t.Fatalf(
			"precondition: a fresh player should hold the speaker "+
				"rate, got %d",
			got,
		)
	}

	if err := p.LoadFile(path); err != nil {
		t.Fatalf("LoadFile(%s): %v", path, err)
	}

	if p.format.SampleRate == speakerSampleRate {
		t.Fatalf(
			"p.format still holds the speaker rate (%d) after "+
				"loading a %d Hz file: the replay path would "+
				"resample from the wrong rate",
			speakerSampleRate, fixtureSampleRate,
		)
	}

	if got := int(p.format.SampleRate); got != fixtureSampleRate {
		t.Errorf(
			"expected the decoder's rate %d, got %d",
			fixtureSampleRate, got,
		)
	}
}

// trackLengthMs is written only when the database has a row for the
// file and cleared only by UnloadTrack, so a track with no row used
// to inherit whatever the last track's duration was -- and every
// position report is scaled by it, so the whole seek bar was then
// reporting one track's progress on another track's scale.
//
// There is no database here, so the lookup cannot succeed: exactly
// the case that used to inherit.
func TestLoadDoesNotInheritThePreviousTracksDuration(t *testing.T) {
	m := testfixtures.Load(t)
	path := m.Case(t, testfixtures.CaseCoverDedup)[0]

	p := newTestPlayer(t)

	// Stand in for a previous track whose duration was resolved.
	p.trackLengthMs = 9_999_000

	if err := p.LoadFile(path); err != nil {
		t.Fatalf("LoadFile(%s): %v", path, err)
	}

	if p.trackLengthMs == 9_999_000 {
		t.Fatal(
			"the previous track's duration survived the load: every " +
				"position report for this track would be scaled by it",
		)
	}
}

// A new chain supersedes the old one's pending finished callback.
// Without this, a callback that queued for p.mu behind a LoadFile
// woke up and rewound, stopped and auto-advanced the *new* track.
func TestANewChainSupersedesTheOldFinishedCallback(t *testing.T) {
	m := testfixtures.Load(t)
	paths := m.Case(t, testfixtures.CaseCoverDedup)

	if len(paths) < 2 {
		t.Skip("need two fixture tracks")
	}

	p := newTestPlayer(t)

	if err := p.LoadFile(paths[0]); err != nil {
		t.Fatalf("LoadFile(%s): %v", paths[0], err)
	}

	stale := p.chainID

	if err := p.LoadFile(paths[1]); err != nil {
		t.Fatalf("LoadFile(%s): %v", paths[1], err)
	}

	if p.chainID == stale {
		t.Fatal("loading a second file did not supersede the chain")
	}

	called := false

	p.SetPlaybackFinishedHandler(func(error) { called = true })

	// The first track's callback, arriving late.
	p.onPlaybackFinished(stale, nil)

	if called {
		t.Error(
			"a superseded chain's callback drove auto-advance: the " +
				"track that is loaded now would be skipped",
		)
	}

	if p.state == Stopped {
		t.Error(
			"a superseded chain's callback stopped the current track",
		)
	}
}

// The decoder is read by the read-ahead goroutine and by every
// position emit, and those used to be guarded by different mutexes:
// the read by srcMu, the position by the speaker lock, which
// read-ahead never takes. Under -race this failed on the emit that
// LoadFile itself makes.
//
// It needs the read-ahead goroutine to actually be running, so it
// keeps asking for the position for long enough to overlap it.
func TestPositionReadsDoNotRaceTheReadAhead(t *testing.T) {
	m := testfixtures.Load(t)
	path := m.Case(t, testfixtures.CaseFLACAlbum)[0]

	p := newTestPlayer(t)

	if err := p.LoadFile(path); err != nil {
		t.Fatalf("LoadFile(%s): %v", path, err)
	}

	for range 200 {
		if _, err := p.CurrentPositionSeconds(); err != nil {
			t.Fatalf("CurrentPositionSeconds: %v", err)
		}
	}
}

// Seeking emits the landing position, and that emit reads the
// decoder -- so the source lock the seek holds must be released
// before it. A reentrant take here is a deadlock, not a failure,
// which is why this test exists rather than a comment.
func TestSeekEmitsWithoutDeadlocking(t *testing.T) {
	m := testfixtures.Load(t)
	path := m.Case(t, testfixtures.CaseFLACAlbum)[0]

	p := newTestPlayer(t)

	if err := p.LoadFile(path); err != nil {
		t.Fatalf("LoadFile(%s): %v", path, err)
	}

	done := make(chan struct{})

	go func() {
		defer close(done)

		_ = p.Seek(1)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Seek deadlocked: the position emit re-took the source lock")
	}
}
