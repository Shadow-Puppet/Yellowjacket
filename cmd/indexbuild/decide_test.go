//go:build indexbuild

package main

import (
	"testing"
	"time"
)

// stubState lets the decision table run without a database. It mirrors
// the three pieces of index state `decide` consults.
type stubState struct {
	complete bool
	last     time.Time
}

func (s stubState) IndexImportComplete() bool    { return s.complete }
func (s stubState) IndexLastImported() time.Time { return s.last }

func TestDecide(t *testing.T) {
	t.Parallel()

	const (
		rebuildAfter = 90 * 24 * time.Hour
		refreshAfter = 7 * 24 * time.Hour
	)

	tests := []struct {
		name  string
		mode  mode
		state stubState
		want  mode
	}{
		{
			name:  "first run with no import builds",
			mode:  modeAuto,
			state: stubState{complete: false},
			want:  modeBuild,
		},
		{
			name:  "partial import resumes as a build",
			mode:  modeAuto,
			state: stubState{complete: false, last: time.Now().Add(-time.Hour)},
			want:  modeBuild,
		},
		{
			name:  "recent import refreshes",
			mode:  modeAuto,
			state: stubState{complete: true, last: time.Now().Add(-24 * time.Hour)},
			want:  modeRefresh,
		},
		{
			name:  "import just under the rebuild age still refreshes",
			mode:  modeAuto,
			state: stubState{complete: true, last: time.Now().Add(-89 * 24 * time.Hour)},
			want:  modeRefresh,
		},
		{
			name:  "import past the rebuild age rebuilds",
			mode:  modeAuto,
			state: stubState{complete: true, last: time.Now().Add(-91 * 24 * time.Hour)},
			want:  modeRebuild,
		},
		{
			name:  "unreadable timestamp rebuilds rather than wedging",
			mode:  modeAuto,
			state: stubState{complete: true},
			want:  modeRebuild,
		},
		{
			name:  "explicit mode overrides state",
			mode:  modeRefresh,
			state: stubState{complete: false},
			want:  modeRefresh,
		},
		{
			name:  "explicit rebuild overrides a fresh import",
			mode:  modeRebuild,
			state: stubState{complete: true, last: time.Now()},
			want:  modeRebuild,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, why := decideFrom(
				opts{
					mode:         tt.mode,
					rebuildAfter: rebuildAfter,
					refreshAfter: refreshAfter,
				},
				tt.state,
			)
			if got != tt.want {
				t.Errorf("decide = %q (%s), want %q", got, why, tt.want)
			}

			if why == "" {
				t.Error("expected a non-empty reason")
			}
		})
	}
}
