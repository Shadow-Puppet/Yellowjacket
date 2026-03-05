package player

import (
	"math"
	"testing"

	"yellowjacket/backend/mediacontrols"
)

func TestUserVolume_ToVolume(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    UserVolume
		expected Volume
	}{
		{"min (0)", MinUserVol, MinVol},
		{"max (100)", MaxUserVol, MaxVol},
		{"default (50)", DefaultUserVol, -2.5},
		{"quarter (25)", 25, -3.75},
		{"three-quarter (75)", 75, -1.25},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.input.ToVolume()
			if math.Abs(float64(got)-float64(tt.expected)) > 0.001 {
				t.Errorf("UserVolume(%d).ToVolume() = %f, want %f", tt.input, got, tt.expected)
			}
		})
	}
}

func TestVolume_ToUserVolume(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    Volume
		expected UserVolume
	}{
		{"min (-5.0)", MinVol, MinUserVol},
		{"max (0.0)", MaxVol, MaxUserVol},
		{"midpoint (-2.5)", -2.5, 50},
		{"quarter (-3.75)", -3.75, 25},
		{"three-quarter (-1.25)", -1.25, 75},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.input.ToUserVolume()
			if got != tt.expected {
				t.Errorf("Volume(%f).ToUserVolume() = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

func TestUserVolume_ToVolume_OutOfRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input UserVolume
	}{
		{"negative (-1)", -1},
		{"over max (101)", 101},
		{"way over (200)", 200},
		{"far negative (-50)", -50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.input.ToVolume()
			// Out-of-range returns zero-value Volume (0.0).
			if got != 0.0 {
				t.Errorf("UserVolume(%d).ToVolume() = %f, want 0.0 (zero-value)", tt.input, got)
			}
		})
	}
}

func TestVolume_ToUserVolume_OutOfRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input Volume
	}{
		{"below min (-6.0)", -6.0},
		{"above max (1.0)", 1.0},
		{"far below (-10.0)", -10.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.input.ToUserVolume()
			// Out-of-range returns zero-value UserVolume (0).
			if got != 0 {
				t.Errorf("Volume(%f).ToUserVolume() = %d, want 0 (zero-value)", tt.input, got)
			}
		})
	}
}

func TestUserVolume_ToVolume_Roundtrip(t *testing.T) {
	t.Parallel()

	// The conversion uses float64 intermediates and int truncation
	// (not rounding), so some values lose 1 unit in the roundtrip.
	// This characterization test verifies the actual behavior:
	// the result is always within ±1 of the original, and boundary
	// values (0, 50, 100) are exact.
	for i := UserVolume(0); i <= 100; i++ {
		vol := i.ToVolume()
		roundtripped := vol.ToUserVolume()

		diff := int(roundtripped) - int(i)
		if diff < -1 || diff > 1 {
			t.Errorf(
				"Roundtrip UserVolume(%d) -> Volume(%f) -> UserVolume(%d): "+
					"drift %d exceeds ±1",
				i, vol, roundtripped, diff,
			)
		}
	}

	// Verify key boundary values are exact.
	exactCases := []UserVolume{MinUserVol, DefaultUserVol, MaxUserVol}
	for _, uv := range exactCases {
		vol := uv.ToVolume()
		roundtripped := vol.ToUserVolume()

		if roundtripped != uv {
			t.Errorf(
				"Exact roundtrip UserVolume(%d) -> Volume(%f) -> "+
					"UserVolume(%d): want exact match",
				uv, vol, roundtripped,
			)
		}
	}
}

func TestClampVolume(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    UserVolume
		expected UserVolume
	}{
		{"far below min", -10, MinUserVol},
		{"at min", 0, 0},
		{"middle", 50, 50},
		{"at max", 100, 100},
		{"above max", 150, MaxUserVol},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := clampVolume(tt.input)
			if got != tt.expected {
				t.Errorf("clampVolume(%d) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

func TestStateToMediaControls(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    State
		expected mediacontrols.PlaybackState
	}{
		{"playing", Playing, mediacontrols.StatePlaying},
		{"paused", Paused, mediacontrols.StatePaused},
		{"stopped", Stopped, mediacontrols.StateStopped},
		{"unknown", State("unknown"), mediacontrols.StateStopped},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := stateToMediaControls(tt.input)
			if got != tt.expected {
				t.Errorf("stateToMediaControls(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}
