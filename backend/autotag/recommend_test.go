package autotag

import "testing"

// mkScoredCandidate builds a minimal candidate with a preset score
// for Recommend tests — Recommend never re-scores, it only reads.
func mkScoredCandidate(rgMBID string, score float64) Candidate {
	return Candidate{
		ReleaseMBID:      "rel-" + rgMBID,
		ReleaseGroupMBID: rgMBID,
		Score:            score,
	}
}

func fullGroup() Group {
	return Group{Tracks: []LocalTrack{{Title: "A"}, {Title: "B"}, {Title: "C"}}}
}

func TestRecommend_Tiers(t *testing.T) {
	t.Parallel()

	g := fullGroup()

	cases := []struct {
		name  string
		cands []Candidate
		want  Recommendation
	}{
		{"no candidates", nil, RecommendationNone},
		{"strong", []Candidate{mkScoredCandidate("rg1", 0.95)}, RecommendationStrong},
		{"medium", []Candidate{mkScoredCandidate("rg1", 0.80)}, RecommendationMedium},
		{"low", []Candidate{mkScoredCandidate("rg1", 0.50)}, RecommendationLow},
	}

	for _, tc := range cases {
		if got := Recommend(g, tc.cands); got != tc.want {
			t.Errorf("%s: Recommend = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestRecommend_AmbiguousRivalCapsAtMedium(t *testing.T) {
	t.Parallel()

	// A different release group within the margin → ambiguous, even
	// though the top score alone reads strong.
	cands := []Candidate{
		mkScoredCandidate("rg1", 0.95),
		mkScoredCandidate("rg2", 0.93),
	}

	if got := Recommend(fullGroup(), cands); got != RecommendationMedium {
		t.Errorf("ambiguous rival: Recommend = %q, want medium", got)
	}
}

func TestRecommend_SameRGEditionsAreNotAmbiguous(t *testing.T) {
	t.Parallel()

	// Multiple editions of the SAME release group score nearly
	// identically by construction — that's not ambiguity.
	cands := []Candidate{
		mkScoredCandidate("rg1", 0.95),
		mkScoredCandidate("rg1", 0.94),
		mkScoredCandidate("rg1", 0.93),
	}

	if got := Recommend(fullGroup(), cands); got != RecommendationStrong {
		t.Errorf("same-RG editions: Recommend = %q, want strong", got)
	}
}

func TestRecommend_DistantRivalDoesNotCap(t *testing.T) {
	t.Parallel()

	cands := []Candidate{
		mkScoredCandidate("rg1", 0.95),
		mkScoredCandidate("rg2", 0.60),
	}

	if got := Recommend(fullGroup(), cands); got != RecommendationStrong {
		t.Errorf("distant rival: Recommend = %q, want strong", got)
	}
}

func TestRecommend_AlignmentDefectsCapAtMedium(t *testing.T) {
	t.Parallel()

	top := mkScoredCandidate("rg1", 0.95)
	top.Alignments = []TrackAlignment{
		{Status: AlignmentMatched},
		{Status: AlignmentMissing, LocalIndex: -1},
	}

	if got := Recommend(fullGroup(), []Candidate{top}); got != RecommendationMedium {
		t.Errorf("missing track: Recommend = %q, want medium", got)
	}
}

func TestRecommend_SyntheticGroupMissingTracksDoNotCap(t *testing.T) {
	t.Parallel()

	top := mkScoredCandidate("rg1", 0.95)
	top.Alignments = []TrackAlignment{
		{Status: AlignmentMatched},
		{Status: AlignmentMissing, LocalIndex: -1},
	}

	g := fullGroup()
	g.Synthetic = true

	if got := Recommend(g, []Candidate{top}); got != RecommendationStrong {
		t.Errorf(
			"synthetic group with only missing (not unmatched) tracks: Recommend = %q, want strong",
			got,
		)
	}
}

func TestRecommend_SyntheticGroupUnmatchedTracksStillCap(t *testing.T) {
	t.Parallel()

	top := mkScoredCandidate("rg1", 0.95)
	top.Alignments = []TrackAlignment{
		{Status: AlignmentMatched},
		{Status: AlignmentUnmatched, LocalIndex: 1},
	}

	g := fullGroup()
	g.Synthetic = true

	if got := Recommend(g, []Candidate{top}); got != RecommendationMedium {
		t.Errorf(
			"synthetic group with an unmatched local track: Recommend = %q, want medium",
			got,
		)
	}
}

func TestRecommend_ThinEvidenceCapsAtMedium(t *testing.T) {
	t.Parallel()

	// A 2-track folder can't be auto-accept confident however well
	// it matches.  (Local candidates skip evidence *scaling* but not
	// this cap — acting without review still needs corroboration.)
	g := Group{Tracks: []LocalTrack{{Title: "A"}, {Title: "B"}}}
	cands := []Candidate{mkScoredCandidate("rg1", 0.96)}

	if got := Recommend(g, cands); got != RecommendationMedium {
		t.Errorf("thin evidence: Recommend = %q, want medium", got)
	}
}

func TestRecommend_LocalCandidatesWithoutRGMBIDCompareByTitle(t *testing.T) {
	t.Parallel()

	// Local candidates may lack RG MBIDs; same title+artist means
	// same release group for ambiguity purposes.
	a := Candidate{Title: "Album", ArtistCredit: "Band", Score: 0.95}
	b := Candidate{Title: "Album", ArtistCredit: "Band", Score: 0.94}

	if got := Recommend(fullGroup(), []Candidate{a, b}); got != RecommendationStrong {
		t.Errorf("same title/artist locals: Recommend = %q, want strong", got)
	}

	c := Candidate{Title: "Different Album", ArtistCredit: "Band", Score: 0.94}
	if got := Recommend(fullGroup(), []Candidate{a, c}); got != RecommendationMedium {
		t.Errorf("different-title rival: Recommend = %q, want medium", got)
	}
}
