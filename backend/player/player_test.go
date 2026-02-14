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
	// This test requires a Wails runtime context for event registration and
	// an audio device for playback. Skip in CI where neither is available.
	if os.Getenv("CI") != "" {
		t.Skip("skipping: requires Wails runtime context and audio device")
	}

	t.Logf("Starting test")

	p, err := NewPlayer(context.Background(), slog.Default(), nil)
	if err != nil {
		t.Errorf("could not create player\n%s", err.Error())
		t.Failed()
	}

	p.SetContext(t.Context())
	t.Logf("initializing player")

	for _, track := range testQueue {
		t.Logf("loading file: %s", track)

		err = p.LoadFile(track)
		if err != nil {
			t.Errorf("could not load file\n%s\n%s", track, err.Error())
			t.Failed()
		}

		err = p.Play()
		if err != nil {
			t.Errorf("could not play file\n%s\n%s", track, err.Error())
			t.Failed()
		}
	}
}
