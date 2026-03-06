package player

import (
	"log/slog"
	"os"
	"testing"
)

var testQueue = []string{
	"../../test_data/music_library_test/other_music/03 PONPONPON.mp3",
	"../../test_data/music_library_test/01 Some Chords.mp3",
	"../../test_data/music_library_test/03 anything.mp3",
}

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
