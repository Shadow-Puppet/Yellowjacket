package autotag

import "testing"

// Autotagging an album used to *erase* the evidence that says "2 of 10":
// the release became MBID-matched while the totals the files declared
// went unwritten, so the album page showed a plain tick.  These pin the
// two halves of the fix that are easy to get wrong silently.
func TestBuildChanges_Totals(t *testing.T) {
	t.Parallel()

	twoDiscs := Candidate{
		Tracks: []CandidateTrack{
			{DiscNumber: 1, Position: 1},
			{DiscNumber: 1, Position: 2},
			{DiscNumber: 2, Position: 1},
			{DiscNumber: 2, Position: 2},
			{DiscNumber: 2, Position: 3},
		},
	}

	tests := []struct {
		name       string
		cand       Candidate
		local      LocalTrack
		track      CandidateTrack
		wantTracks any
		wantDiscs  any
	}{
		{
			// The common case, and the one a diff guard would skip: the
			// file declares no total at all, so the total "has not
			// changed" and would never be written.
			name: "a file with no total gets one",
			cand: Candidate{Tracks: []CandidateTrack{
				{Position: 1}, {Position: 2}, {Position: 3},
			}},
			local:      LocalTrack{TrackNumber: 1},
			track:      CandidateTrack{Position: 1},
			wantTracks: 3,
			wantDiscs:  1,
		},
		{
			// 5 here would be the release's track count.  Summed once
			// per disc by GetAlbumCompleteness that claims a ten-track
			// expectation for a five-track album, which no library can
			// ever satisfy.
			name:       "a multi-disc release totals the track's own disc",
			cand:       twoDiscs,
			local:      LocalTrack{},
			track:      CandidateTrack{DiscNumber: 2, Position: 1},
			wantTracks: 3,
			wantDiscs:  2,
		},
		{
			name:       "the other disc gets its own total",
			cand:       twoDiscs,
			local:      LocalTrack{},
			track:      CandidateTrack{DiscNumber: 1, Position: 1},
			wantTracks: 2,
			wantDiscs:  2,
		},
		{
			// A candidate with no tracklist knows nothing, and writing
			// a zero would claim it did.
			name:       "a candidate with no tracklist writes no total",
			cand:       Candidate{},
			local:      LocalTrack{},
			track:      CandidateTrack{Position: 1},
			wantTracks: nil,
			wantDiscs:  nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			changes := buildChanges(tc.local, tc.cand, tc.track)

			if got := changes[FieldTotalTracks]; got != tc.wantTracks {
				t.Errorf("%s: got %v, want %v", FieldTotalTracks, got, tc.wantTracks)
			}

			if got := changes[FieldTotalDiscs]; got != tc.wantDiscs {
				t.Errorf("%s: got %v, want %v", FieldTotalDiscs, got, tc.wantDiscs)
			}
		})
	}
}
