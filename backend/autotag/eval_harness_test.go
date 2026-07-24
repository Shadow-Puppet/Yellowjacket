package autotag_test

import (
	"testing"

	"yellowjacket/backend/autotag"
	"yellowjacket/backend/autotag/eval"
)

// adaptRanker converts an eval.Case (hand-written fixture shapes) into
// the autotag domain types, runs the real RankCandidates, and maps
// the result back to []eval.ScoredCandidate.  This is the one place
// the eval package's decoupled fixtures meet the concrete scorer.
func adaptRanker(c eval.Case) []eval.ScoredCandidate {
	locals := make([]autotag.LocalTrack, len(c.Local))
	for i, l := range c.Local {
		locals[i] = autotag.LocalTrack{
			Title:        l.Title,
			Artist:       l.Artist,
			TrackNumber:  l.Track,
			DiscNumber:   l.Disc,
			LengthMillis: l.LengthMs,
		}
	}

	cands := make([]autotag.Candidate, len(c.Candidates))
	for i, cf := range c.Candidates {
		tracks := make([]autotag.CandidateTrack, len(cf.Tracks))
		for j, tf := range cf.Tracks {
			tracks[j] = autotag.CandidateTrack{
				Position:     tf.Pos,
				DiscNumber:   tf.Disc,
				Title:        tf.Title,
				LengthMillis: tf.LengthMs,
			}
		}

		cands[i] = autotag.Candidate{
			ReleaseMBID:  cf.MBID,
			Title:        cf.Title,
			ArtistCredit: cf.ArtistCredit,
			Status:       cf.Status,
			Country:      cf.Country,
			PrimaryType:  cf.PrimaryType,
			Source:       candidateSource(cf.Source),
			Tracks:       tracks,
		}
	}

	ranked := autotag.RankCandidates(autotag.Group{
		AlbumName:   c.AlbumName,
		AlbumArtist: c.AlbumArtist,
		Tracks:      locals,
	}, cands)

	out := make([]eval.ScoredCandidate, len(ranked))
	for i, r := range ranked {
		out[i] = eval.ScoredCandidate{MBID: r.ReleaseMBID, Score: r.Score}
	}

	return out
}

// candidateSource maps the fixture string to the domain type,
// defaulting to MusicBrainz (the interesting, evidence-scaled path).
func candidateSource(s string) autotag.CandidateSource {
	if s == string(autotag.SourceLocal) {
		return autotag.SourceLocal
	}

	return autotag.SourceMusicBrainz
}

// TestScoringCorpus runs the frozen labelled corpus through the real
// ranker.  Add real-world mismatches to testdata/scoring_cases.json —
// every case that fails here is a scoring regression, and the harness
// prints exactly which expectation broke.
func TestScoringCorpus(t *testing.T) {
	t.Parallel()

	cases, err := eval.LoadCases("eval/testdata/scoring_cases.json")
	if err != nil {
		t.Fatalf("load cases: %v", err)
	}

	report := eval.Evaluate(cases, eval.RankerFunc(adaptRanker))

	for _, r := range report.Results {
		if r.Passed() {
			continue
		}

		for _, f := range r.Failures {
			t.Errorf("case %q: %s", r.Case.Note, f)
		}
	}

	t.Logf(
		"scoring corpus: %d/%d cases passed (%.0f%% accuracy)",
		report.Passed(), len(report.Results), report.Accuracy()*100,
	)
}
