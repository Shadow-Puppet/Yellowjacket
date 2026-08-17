package mediacontrols

import (
	"encoding/json"
	"testing"
)

// TestMediaPayloadKeys pins the document the Java side parses. The
// keys are the contract: a rename here is silently a track with no
// title on the lock screen, because WailsForegroundService reads them
// with optString and a missing key is simply "".
func TestMediaPayloadKeys(t *testing.T) {
	t.Parallel()

	payload, err := mediaPayload(Metadata{
		Title:       "Tideline",
		Artist:      "Sea Change",
		Album:       "Ebb",
		ArtFilePath: "/covers/ebb_lg.jpg",
		DurationSec: 245,
	}, StatePlaying, 30)
	if err != nil {
		t.Fatalf("mediaPayload: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}

	want := map[string]any{
		"title":       "Tideline",
		"artist":      "Sea Change",
		"album":       "Ebb",
		"artPath":     "/covers/ebb_lg.jpg",
		"durationSec": float64(245),
		"positionSec": float64(30),
		"state":       "playing",
	}

	if len(got) != len(want) {
		t.Errorf("payload has %d keys, want %d: %s", len(got), len(want), payload)
	}

	for key, expected := range want {
		if got[key] != expected {
			t.Errorf("payload[%q] = %v, want %v", key, got[key], expected)
		}
	}
}

// TestMediaPayloadStateNames covers the one value the Java side
// compares against a literal.
func TestMediaPayloadStateNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state PlaybackState
		want  string
	}{
		{StatePlaying, "playing"},
		{StatePaused, "paused"},
		{StateStopped, "stopped"},
	}

	for _, tt := range tests {
		payload, err := mediaPayload(Metadata{}, tt.state, 0)
		if err != nil {
			t.Fatalf("mediaPayload: %v", err)
		}

		var got struct {
			State string `json:"state"`
		}

		if err := json.Unmarshal([]byte(payload), &got); err != nil {
			t.Fatalf("payload is not JSON: %v", err)
		}

		if got.State != tt.want {
			t.Errorf("state %d encoded as %q, want %q", tt.state, got.State, tt.want)
		}
	}
}

// TestParseMediaCommand covers the direction that arrives untyped.
// The seek case is the one with teeth: the position crosses as a JSON
// number, so it is a float64 in the map and an int assertion would
// make every seek a seek to zero.
func TestParseMediaCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data map[string]any
		want mediaCommand
	}{
		{
			name: "play",
			data: map[string]any{"command": "play"},
			want: mediaCommand{name: cmdPlay},
		},
		{
			name: "seek carries a position",
			data: map[string]any{"command": "seek", "positionSec": float64(93)},
			want: mediaCommand{name: cmdSeek, positionSec: 93},
		},
		{
			name: "duck carries a flag",
			data: map[string]any{"command": "duck", "on": true},
			want: mediaCommand{name: cmdDuck, duck: true},
		},
		{
			name: "unduck",
			data: map[string]any{"command": "duck", "on": false},
			want: mediaCommand{name: cmdDuck},
		},
		{
			name: "a command with nothing in it is not a panic",
			data: map[string]any{},
			want: mediaCommand{},
		},
		{
			name: "wrongly typed fields fall back to zero",
			data: map[string]any{"command": "seek", "positionSec": "93"},
			want: mediaCommand{name: cmdSeek},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := parseMediaCommand(tt.data); got != tt.want {
				t.Errorf("parseMediaCommand(%v) = %+v, want %+v", tt.data, got, tt.want)
			}
		})
	}
}

// TestMediaCommandNamesAreWhatJavaSends is a spelling check against
// the Java side, which builds these strings by hand. It is a list, not
// a mechanism: nothing can reach across into the .java file, so the
// point is that changing one of these constants fails a test that
// names the file to change with it.
//
// See build/android/app/src/main/java/com/wails/app/WailsForegroundService.java.
func TestMediaCommandNamesAreWhatJavaSends(t *testing.T) {
	t.Parallel()

	want := []string{
		"play", "pause", "playpause", "stop",
		"next", "previous", "seek", "duck",
	}
	got := []string{
		cmdPlay, cmdPause, cmdPlayPause, cmdStop,
		cmdNext, cmdPrevious, cmdSeek, cmdDuck,
	}

	for i, name := range want {
		if got[i] != name {
			t.Errorf("command %d = %q, want %q", i, got[i], name)
		}
	}
}
