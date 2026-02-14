package player

import (
	"context"
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
	//   1. A Wails runtime context (SetContext calls runtime.EventsOn)
	//   2. An audio output device (speaker.Init)
	//
	// Skip unless the caller explicitly opts in via YELLOWJACKET_INTEGRATION=1.
	if os.Getenv("YELLOWJACKET_INTEGRATION") == "" {
		t.Skip(
			"skipping: integration test requires Wails runtime and audio device (set YELLOWJACKET_INTEGRATION=1 to run)",
		)
	}

	t.Logf("Starting test")

	p, err := NewPlayer(context.Background(), slog.Default(), nil)
	if err != nil {
		t.Fatalf("could not create player: %s", err.Error())
	}

	// SetContext registers Wails event handlers; only works with a real Wails context.
	p.SetContext(t.Context())
	t.Logf("initializing player")

	for _, track := range testQueue {
		t.Logf("loading file: %s", track)

		err = p.LoadFile(track)
		if err != nil {
			t.Fatalf("could not load file %s: %s", track, err.Error())
		}

		err = p.Play()
		if err != nil {
			t.Fatalf("could not play file %s: %s", track, err.Error())
		}
	}
}
