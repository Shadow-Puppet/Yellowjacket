package tagtotals_test

import (
	"testing"

	"yellowjacket/backend/tagtotals"
)

func TestFor(t *testing.T) {
	t.Parallel()

	singleDisc := []tagtotals.Position{
		{Disc: 0, Track: 1}, {Disc: 0, Track: 2}, {Disc: 0, Track: 3},
	}

	twoDiscs := []tagtotals.Position{
		{Disc: 1, Track: 1},
		{Disc: 1, Track: 2},
		{Disc: 2, Track: 1},
		{Disc: 2, Track: 2},
		{Disc: 2, Track: 3},
	}

	tests := []struct {
		name       string
		all        []tagtotals.Position
		disc       int
		wantTracks int
		wantDiscs  int
	}{
		{
			name: "a single-disc release totals its own tracks",
			all:  singleDisc, disc: 0, wantTracks: 3, wantDiscs: 1,
		},
		{
			// An undeclared disc is disc 1, on both sides of the
			// question -- a file tagged "disc 1" and a tracklist that
			// declares no disc describe the same disc.
			name: "an undeclared disc is disc 1",
			all:  singleDisc, disc: 1, wantTracks: 3, wantDiscs: 1,
		},
		{
			// The whole point: 5 here would be the release's track
			// count, which summed once per disc claims a ten-track
			// expectation for a five-track album.
			name: "a multi-disc release totals the file's own disc",
			all:  twoDiscs, disc: 2, wantTracks: 3, wantDiscs: 2,
		},
		{
			name: "the other disc gets its own total",
			all:  twoDiscs, disc: 1, wantTracks: 2, wantDiscs: 2,
		},
		{
			// A disc the tracklist does not mention cannot be totalled,
			// and 0 is how the caller is told to write nothing.
			name: "a disc with no tracks totals nothing",
			all:  twoDiscs, disc: 3, wantTracks: 0, wantDiscs: 2,
		},
		{
			name: "an empty tracklist totals nothing",
			all:  nil, disc: 1, wantTracks: 0, wantDiscs: 0,
		},
		{
			// A source that lists a position twice would otherwise put
			// the album permanently one track short of its own total.
			name: "a repeated position counts once",
			all: []tagtotals.Position{
				{Disc: 1, Track: 1}, {Disc: 1, Track: 1}, {Disc: 1, Track: 2},
			},
			disc: 1, wantTracks: 2, wantDiscs: 1,
		},
		{
			name: "a track with no position is not counted",
			all: []tagtotals.Position{
				{Disc: 1, Track: 0}, {Disc: 1, Track: 1},
			},
			disc: 1, wantTracks: 1, wantDiscs: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tracks, discs := tagtotals.For(tc.all, tc.disc)
			if tracks != tc.wantTracks || discs != tc.wantDiscs {
				t.Errorf("For(%v, %d) = (%d, %d), want (%d, %d)",
					tc.all, tc.disc, tracks, discs, tc.wantTracks, tc.wantDiscs)
			}
		})
	}
}
