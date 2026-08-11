package player

import (
	"log/slog"
	"os"
	"testing"

	"yellowjacket/internal/testfixtures"
)

func TestPlayer(t *testing.T) {
	// This is an integration test that requires:
	//   1. A Wails runtime context (SetContext restores persisted state)
	//   2. An audio output device (InitSpeaker)
	//
	// Skip unless the caller explicitly opts in via YELLOWJACKET_INTEGRATION=1.
	if os.Getenv("YELLOWJACKET_INTEGRATION") == "" {
		t.Skip(
			"skipping: integration test requires Wails runtime and audio device (set YELLOWJACKET_INTEGRATION=1 to run)",
		)
	}

	// One track per supported container, so a decoder regression in
	// any of the four shows up here rather than only in whichever
	// format the fixtures happened to lead with.
	m := testfixtures.Load(t)
	testQueue := []string{
		m.Case(t, testfixtures.CaseCoverDedup)[0],
		m.Case(t, testfixtures.CaseFLACAlbum)[0],
		m.Case(t, testfixtures.CaseOGGAlbum)[0],
		m.Case(t, testfixtures.CaseWAVTracks)[0],
	}

	t.Logf("Starting test")

	p := NewPlayer(slog.Default(), nil)

	if err := p.InitSpeaker(); err != nil {
		t.Fatalf("could not initialize speaker: %s", err.Error())
	}

	// SetContext restores persisted state; only works with a real Wails context.
	p.SetContext(t.Context())
	t.Logf("initializing player")

	for _, track := range testQueue {
		t.Logf("loading file: %s", track)

		if err := p.LoadFile(track); err != nil {
			t.Fatalf("could not load file %s: %s", track, err.Error())
		}

		if err := p.Play(); err != nil {
			t.Fatalf("could not play file %s: %s", track, err.Error())
		}
	}
}
