package download

import "testing"

// okComputer is the reference request used across ranking tests.
func okComputer() Request {
	return Request{
		ReleaseMBID: "mbid-ok-computer",
		Artist:      "Radiohead",
		Album:       "OK Computer",
		Expected: []ExpectedTrack{
			{Position: 1, Title: "Airbag"},
			{Position: 2, Title: "Paranoid Android"},
			{Position: 3, Title: "Subterranean Homesick Alien"},
			{Position: 4, Title: "Exit Music (For a Film)"},
		},
	}
}

// candidateFor builds a candidate whose files follow "NN - Title.ext".
func candidateFor(id string, titles []string, ext string, size int64) Candidate {
	files := make([]CandidateFile, 0, len(titles))

	for i, tt := range titles {
		files = append(files, CandidateFile{
			Path: "Radiohead - OK Computer/" +
				trackToken(i+1) + " - " + tt + ext,
			Size: size,
		})
	}

	return Candidate{
		ID:       id,
		Protocol: ProtocolDirect,
		Title:    "Radiohead - OK Computer",
		Artist:   "Radiohead",
		Files:    files,
		Health:   0.5,
	}
}

func allTitles() []string {
	return []string{
		"Airbag",
		"Paranoid Android",
		"Subterranean Homesick Alien",
		"Exit Music (For a Film)",
	}
}

// The headline behaviour: a well-matched FLAC beats a well-matched
// 128kbps MP3, but a mismatched FLAC loses to both.
func TestRankPrefersQualityAtEqualMatch(t *testing.T) {
	t.Parallel()

	req := okComputer()

	flac := candidateFor("flac", allTitles(), ".flac", 30_000_000)
	mp3 := candidateFor("mp3", allTitles(), ".mp3", 3_000_000)

	for i := range mp3.Files {
		mp3.Files[i].Bitrate = 128
	}

	ranked := Rank(req, []Candidate{mp3, flac}, nil)

	if ranked[0].ID != "flac" {
		t.Fatalf("winner = %s, want flac", ranked[0].ID)
	}

	if ranked[0].Match.Overall < 0.9 {
		t.Errorf("flac match = %f, want high", ranked[0].Match.Overall)
	}

	if ranked[0].Quality.Overall <= ranked[1].Quality.Overall {
		t.Errorf(
			"flac quality %f should exceed mp3 %f",
			ranked[0].Quality.Overall, ranked[1].Quality.Overall,
		)
	}
}

func TestRankMatchDominatesQuality(t *testing.T) {
	t.Parallel()

	req := okComputer()

	// Right album, poor bitrate.
	right := candidateFor("right", allTitles(), ".mp3", 2_000_000)
	for i := range right.Files {
		right.Files[i].Bitrate = 128
	}

	// Wrong album, pristine FLAC.
	wrong := candidateFor("wrong", []string{
		"Enter Sandman", "Sad But True", "Holier Than Thou", "The Unforgiven",
	}, ".flac", 30_000_000)
	wrong.Title = "Metallica - Metallica"
	wrong.Artist = "Metallica"

	for i := range wrong.Files {
		wrong.Files[i].Path = "Metallica - Metallica/" +
			trackToken(i+1) + " - x.flac"
	}

	ranked := Rank(req, []Candidate{wrong, right}, nil)

	if ranked[0].ID != "right" {
		t.Fatalf(
			"winner = %s (score %f vs %f), want the correctly matched album",
			ranked[0].ID, ranked[0].Score, ranked[1].Score,
		)
	}
}

func TestIncompleteCandidateScoresLower(t *testing.T) {
	t.Parallel()

	req := okComputer()

	full := candidateFor("full", allTitles(), ".flac", 30_000_000)
	partial := candidateFor("partial", allTitles()[:2], ".flac", 30_000_000)

	ranked := Rank(req, []Candidate{partial, full}, nil)

	if ranked[0].ID != "full" {
		t.Fatalf("winner = %s, want full", ranked[0].ID)
	}

	if ranked[1].Match.Completeness >= ranked[0].Match.Completeness {
		t.Errorf(
			"partial completeness %f should be below full %f",
			ranked[1].Match.Completeness, ranked[0].Match.Completeness,
		)
	}
}

func TestMixedFormatIsPenalized(t *testing.T) {
	t.Parallel()

	req := okComputer()

	clean := candidateFor("clean", allTitles(), ".flac", 30_000_000)

	mixed := candidateFor("mixed", allTitles(), ".flac", 30_000_000)
	mixed.Files[2].Path = "Radiohead - OK Computer/03 - x.mp3"
	mixed.Files[2].Format = FormatUnknown

	ranked := Rank(req, []Candidate{mixed, clean}, nil)

	var mixedScore QualityScore

	for _, c := range ranked {
		if c.ID == "mixed" {
			mixedScore = c.Quality
		}
	}

	if !mixedScore.Mixed {
		t.Error("mixed-format candidate not flagged")
	}

	if ranked[0].ID != "clean" {
		t.Errorf("winner = %s, want clean", ranked[0].ID)
	}
}

// Without an MBID there is no tracklist to be right about, so the match
// score must not look confident regardless of how good the strings are.
func TestUnanchoredMatchIsCapped(t *testing.T) {
	t.Parallel()

	req := Request{Artist: "Radiohead", Album: "OK Computer"}
	c := candidateFor("c", allTitles(), ".flac", 30_000_000)

	scored := Score(req, c, 50)

	if scored.Match.Anchored {
		t.Error("free-text request reported as anchored")
	}

	if scored.Match.Overall > unanchoredCap {
		t.Errorf(
			"unanchored match = %f, want <= %f",
			scored.Match.Overall, unanchoredCap,
		)
	}
}

func TestAutoPickableRequiresAnchorAndLead(t *testing.T) {
	t.Parallel()

	req := okComputer()
	best := Score(req, candidateFor("a", allTitles(), ".flac", 30_000_000), 50)

	t.Run("clear winner is auto-pickable", func(t *testing.T) {
		t.Parallel()

		weak := Score(
			req,
			candidateFor("b", allTitles()[:2], ".mp3", 1_000_000),
			50,
		)

		if !AutoPickable(req, []Candidate{best, weak}) {
			t.Errorf(
				"want auto-pickable: match %f quality %f lead %f",
				best.Match.Overall, best.Quality.Overall, best.Score-weak.Score,
			)
		}
	})

	t.Run("two close candidates are not", func(t *testing.T) {
		t.Parallel()

		twin := best
		twin.ID = "twin"

		if AutoPickable(req, []Candidate{best, twin}) {
			t.Error("identical candidates must not auto-pick")
		}
	})

	t.Run("free text is never auto-pickable", func(t *testing.T) {
		t.Parallel()

		free := Request{Artist: "Radiohead", Album: "OK Computer"}

		if AutoPickable(free, []Candidate{best}) {
			t.Error("unanchored request must not auto-pick")
		}
	})

	t.Run("empty list is not", func(t *testing.T) {
		t.Parallel()

		if AutoPickable(req, nil) {
			t.Error("empty candidate list must not auto-pick")
		}
	})
}

func TestCompleteness(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		got      int
		want     int
		minScore float64
		maxScore float64
	}{
		{"exact", 10, 10, 1.0, 1.0},
		{"half missing", 5, 10, 0.49, 0.51},
		{"one bonus track", 11, 10, 0.95, 1.0},
		{"double", 20, 10, 0.74, 0.76},
		{"nothing", 0, 10, 0, 0},
		{"no expectation", 5, 0, 0.5, 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := completeness(tt.got, tt.want)
			if got < tt.minScore || got > tt.maxScore {
				t.Errorf(
					"completeness(%d, %d) = %f, want in [%f, %f]",
					tt.got, tt.want, got, tt.minScore, tt.maxScore,
				)
			}
		})
	}
}

func TestProviderPriorityBreaksTies(t *testing.T) {
	t.Parallel()

	req := okComputer()

	a := candidateFor("a", allTitles(), ".flac", 30_000_000)
	a.ProviderID = 1

	b := candidateFor("b", allTitles(), ".flac", 30_000_000)
	b.ProviderID = 2

	priority := func(id int64) int {
		if id == 2 {
			return 90
		}

		return 10
	}

	ranked := Rank(req, []Candidate{a, b}, priority)

	if ranked[0].ID != "b" {
		t.Errorf("winner = %s, want b (higher provider priority)", ranked[0].ID)
	}
}
