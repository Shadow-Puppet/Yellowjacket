package download

import "testing"

// okComputer is the reference request used across ranking tests.
func okComputer() Download {
	return Download{
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

	dl := okComputer()

	flac := candidateFor("flac", allTitles(), ".flac", 30_000_000)
	mp3 := candidateFor("mp3", allTitles(), ".mp3", 3_000_000)

	for i := range mp3.Files {
		mp3.Files[i].Bitrate = 128
	}

	ranked := Rank(dl, []Candidate{mp3, flac}, nil, AutoDownloadPrefs{})

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

	dl := okComputer()

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

	ranked := Rank(dl, []Candidate{wrong, right}, nil, AutoDownloadPrefs{})

	if ranked[0].ID != "right" {
		t.Fatalf(
			"winner = %s (score %f vs %f), want the correctly matched album",
			ranked[0].ID, ranked[0].Score, ranked[1].Score,
		)
	}
}

func TestIncompleteCandidateScoresLower(t *testing.T) {
	t.Parallel()

	dl := okComputer()

	full := candidateFor("full", allTitles(), ".flac", 30_000_000)
	partial := candidateFor("partial", allTitles()[:2], ".flac", 30_000_000)

	ranked := Rank(dl, []Candidate{partial, full}, nil, AutoDownloadPrefs{})

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

	dl := okComputer()

	clean := candidateFor("clean", allTitles(), ".flac", 30_000_000)

	mixed := candidateFor("mixed", allTitles(), ".flac", 30_000_000)
	mixed.Files[2].Path = "Radiohead - OK Computer/03 - x.mp3"
	mixed.Files[2].Format = FormatUnknown

	ranked := Rank(dl, []Candidate{mixed, clean}, nil, AutoDownloadPrefs{})

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

	dl := Download{Artist: "Radiohead", Album: "OK Computer"}
	c := candidateFor("c", allTitles(), ".flac", 30_000_000)

	scored := Score(dl, c, 50, AutoDownloadPrefs{})

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

	dl := okComputer()
	best := Score(dl, candidateFor("a", allTitles(), ".flac", 30_000_000), 50, AutoDownloadPrefs{})

	t.Run("clear winner is auto-pickable", func(t *testing.T) {
		t.Parallel()

		weak := Score(
			dl,
			candidateFor("b", allTitles()[:2], ".mp3", 1_000_000),
			50,
			AutoDownloadPrefs{},
		)

		if !AutoPickable(dl, []Candidate{best, weak}, AutoDownloadPrefs{}) {
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

		if AutoPickable(dl, []Candidate{best, twin}, AutoDownloadPrefs{}) {
			t.Error("identical candidates must not auto-pick")
		}
	})

	t.Run("free text is never auto-pickable", func(t *testing.T) {
		t.Parallel()

		free := Download{Artist: "Radiohead", Album: "OK Computer"}

		if AutoPickable(free, []Candidate{best}, AutoDownloadPrefs{}) {
			t.Error("unanchored request must not auto-pick")
		}
	})

	t.Run("empty list is not", func(t *testing.T) {
		t.Parallel()

		if AutoPickable(dl, nil, AutoDownloadPrefs{}) {
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

	dl := okComputer()

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

	ranked := Rank(dl, []Candidate{a, b}, priority, AutoDownloadPrefs{})

	if ranked[0].ID != "b" {
		t.Errorf("winner = %s, want b (higher provider priority)", ranked[0].ID)
	}
}

const mb = 1 << 20

func TestAutoDownloadPrefsEligible(t *testing.T) {
	t.Parallel()

	flacCandidate := candidateFor("c", allTitles(), ".flac", 30_000_000)
	flacCandidate.Files = AnnotateFiles(flacCandidate.Files)
	flacCandidate.TotalSize = 300 * mb

	mp3Candidate := candidateFor("c", allTitles(), ".mp3", 3_000_000)
	mp3Candidate.Files = AnnotateFiles(mp3Candidate.Files)
	mp3Candidate.TotalSize = 30 * mb

	tests := []struct {
		name  string
		prefs AutoDownloadPrefs
		c     Candidate
		want  bool
	}{
		{"zero value is permissive", AutoDownloadPrefs{}, flacCandidate, true},
		{
			"within min/max window",
			AutoDownloadPrefs{MinSizeMB: 100, MaxSizeMB: 500},
			flacCandidate, true,
		},
		{
			"below minimum",
			AutoDownloadPrefs{MinSizeMB: 400},
			flacCandidate, false,
		},
		{
			"above maximum",
			AutoDownloadPrefs{MaxSizeMB: 200},
			flacCandidate, false,
		},
		{
			"allowed format passes",
			AutoDownloadPrefs{AllowedFormats: []Format{FormatFLAC}},
			flacCandidate, true,
		},
		{
			"disallowed format rejected",
			AutoDownloadPrefs{AllowedFormats: []Format{FormatFLAC}},
			mp3Candidate, false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.prefs.eligible(tt.c); got != tt.want {
				t.Errorf("eligible() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAutoDownloadPrefsFilter(t *testing.T) {
	t.Parallel()

	small := candidateFor("small", allTitles(), ".flac", 10_000_000)
	small.TotalSize = 50 * mb

	big := candidateFor("big", allTitles(), ".flac", 30_000_000)
	big.TotalSize = 500 * mb

	prefs := AutoDownloadPrefs{MinSizeMB: 100, MaxSizeMB: 600}

	filtered := prefs.filter([]Candidate{small, big})

	if len(filtered) != 1 || filtered[0].ID != "big" {
		t.Errorf("filter() = %v, want only the in-window candidate", filtered)
	}
}

func TestAutoDownloadPrefsSizeFit(t *testing.T) {
	t.Parallel()

	const neutral = 0.5

	tests := []struct {
		name      string
		prefs     AutoDownloadPrefs
		totalSize int64
		want      float64
	}{
		{"no preference is neutral", AutoDownloadPrefs{}, 300 * mb, neutral},
		{
			"exact match scores 1",
			AutoDownloadPrefs{PreferredSizeMB: 300},
			300 * mb, 1.0,
		},
		{
			"double the preferred size scores 0",
			AutoDownloadPrefs{PreferredSizeMB: 300},
			600 * mb, 0.0,
		},
		{
			"half the preferred size scores 0",
			AutoDownloadPrefs{PreferredSizeMB: 300},
			150 * mb, 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.prefs.sizeFit(tt.totalSize); got != tt.want {
				t.Errorf("sizeFit(%d) = %f, want %f", tt.totalSize, got, tt.want)
			}
		})
	}
}

// An otherwise-perfect candidate must not auto-pick when it falls
// outside the configured size guard: the guardrail applies before the
// match/quality/lead checks, not as one more input averaged into them.
func TestAutoPickableRejectsCandidateOutsideSizeGuard(t *testing.T) {
	t.Parallel()

	dl := okComputer()
	best := Score(dl, candidateFor("a", allTitles(), ".flac", 30_000_000), 50, AutoDownloadPrefs{})
	best.TotalSize = 500 * mb

	if !AutoPickable(dl, []Candidate{best}, AutoDownloadPrefs{}) {
		t.Fatal("expected this candidate to be auto-pickable with no guardrails")
	}

	tight := AutoDownloadPrefs{MinSizeMB: 10, MaxSizeMB: 100}

	if AutoPickable(dl, []Candidate{best}, tight) {
		t.Error("candidate outside the size guard must not auto-pick")
	}
}
