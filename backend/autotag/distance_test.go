package autotag

import "testing"

func TestLevenshtein(t *testing.T) {
	t.Parallel()

	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"kitten", "sitting", 3},
		{"beyoncé", "beyonce", 1},
		{"abbey road", "abbey road", 0},
	}

	for _, tc := range cases {
		got := levenshtein(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestTitleSimilarity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		a, b     string
		minScore float64
	}{
		{"Hey Jude", "Hey Jude", 1.00},
		{"Hey Jude", "Hey Jude (Remastered 2009)", 1.00}, // whitelisted qualifier: free
		{"Hey Jude", "Hey Jude - 2015 Remaster", 1.00},   // dash-suffix qualifier: free
		{"Hey Jude", "HEY JUDE!", 1.00},                  // case + punct
		{"Beyoncé", "Beyonce", 1.00},                     // transliteration
		{"Simon & Garfunkel", "Simon and Garfunkel", 1.00},
		{"Beatles, The", "The Beatles", 1.00}, // article rotation
		{"Hey Jude", "Hay Jude", 0.85},        // one char off
		// Unknown parenthetical content is de-weighted, not free —
		// still similar, but detectably not identical.
		{"Song Title (Special Whatever)", "Song Title", 0.80},
		{"Yellow (feat. Somebody)", "Yellow", 0.90}, // feat credit nearly free
		{"Hey Jude", "Let It Be", 0.00},             // different
	}

	for _, tc := range cases {
		got := titleSimilarity(tc.a, tc.b)
		if tc.minScore == 0 {
			if got > 0.5 { //nolint:mnd
				t.Errorf("titleSimilarity(%q, %q) = %.2f, expected low", tc.a, tc.b, got)
			}

			continue
		}

		if got < tc.minScore {
			t.Errorf(
				"titleSimilarity(%q, %q) = %.2f, want >= %.2f",
				tc.a, tc.b, got, tc.minScore,
			)
		}
	}
}

func TestLengthScore(t *testing.T) {
	t.Parallel()

	cases := []struct {
		local, cand int64
		want        float64
	}{
		{200000, 200000, 1.0}, // exact
		{200000, 200500, 1.0}, // 0.5s — under the grace band
		{200000, 204900, 1.0}, // 4.9s — under the grace band
		{200000, 205000, 1.0}, // exactly 5s — still full credit
		{0, 200000, 0.5},      // unknown local → neutral
		{200000, 0, 0.5},      // unknown candidate → neutral

		// Past 2s, score scales by delta / candidateMs.  20% of
		// candidate length = fully wrong (0.0).
		// 240s candidate, 12s delta = 5% → 1 - 5/20 = 0.75.
		{240000 + 12000, 240000, 0.75},
		// 240s candidate, 24s delta = 10% → 1 - 10/20 = 0.50.
		{240000 + 24000, 240000, 0.50},
		// 240s candidate, 48s delta = 20% → clamped to 0.
		{240000 + 48000, 240000, 0.0},
		// 240s candidate, 72s delta = 30% → still 0 (clamped).
		{240000 + 72000, 240000, 0.0},

		// Same absolute delta hits short tracks harder.
		// 60s candidate, 6s delta = 10% → 0.50.
		{66000, 60000, 0.50},
		// 60s candidate, 12s delta = 20% → 0.0.
		{72000, 60000, 0.0},
	}

	for _, tc := range cases {
		got := lengthScore(tc.local, tc.cand)
		if diff := got - tc.want; diff < -0.05 || diff > 0.05 { //nolint:mnd
			t.Errorf(
				"lengthScore(%d, %d) = %.3f, want %.3f ± 0.05",
				tc.local, tc.cand, got, tc.want,
			)
		}
	}
}

func TestTrackDistance(t *testing.T) {
	t.Parallel()

	local := LocalTrack{
		Title:        "Hey Jude",
		TrackNumber:  1,
		LengthMillis: 431000,
	}

	exact := CandidateTrack{
		Position:     1,
		Title:        "Hey Jude",
		LengthMillis: 431000,
	}

	if got := trackDistance(local, exact); got < 0.99 {
		t.Errorf("exact match = %.2f, want ~1.0", got)
	}

	wrong := CandidateTrack{
		Position:     5,
		Title:        "Yesterday",
		LengthMillis: 125000,
	}

	if got := trackDistance(local, wrong); got > 0.20 {
		t.Errorf("wrong match = %.2f, want < 0.2", got)
	}
}

func TestDominantArtist(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		local []LocalTrack
		want  string
	}{
		{"empty", nil, ""},
		{"all blank", []LocalTrack{{Artist: ""}, {Artist: ""}}, ""},
		{
			"unanimous",
			[]LocalTrack{{Artist: "Radiohead"}, {Artist: "Radiohead"}},
			"Radiohead",
		},
		{
			"majority wins over a stray",
			[]LocalTrack{{Artist: "Radiohead"}, {Artist: "Radiohead"}, {Artist: "Guest"}},
			"Radiohead",
		},
		{
			"blanks ignored, one real value wins",
			[]LocalTrack{{Artist: ""}, {Artist: "Bjork"}, {Artist: ""}},
			"Bjork",
		},
	}

	for _, tc := range cases {
		if got := dominantArtist(tc.local); got != tc.want {
			t.Errorf("%s: dominantArtist = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestArtistCreditFit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		local, cnd string
		wantMin    float64 // when > 0, require >= ; when 0, require <= 0.5 (mismatch)
	}{
		{"unknown local is neutral", "", "Whoever", 1.0},
		{"unknown candidate is neutral", "Whoever", "", 1.0},
		{"exact", "The Beatles", "The Beatles", 1.0},
		{"case/punct only", "The Beatles", "THE BEATLES!", 1.0},
		{"almost-right stays high", "Beyonce", "Beyoncé", 0.85},
		{"completely different artist", "The Beatles", "Metallica", 0.0},
	}

	for _, tc := range cases {
		got := artistCreditFit(tc.local, tc.cnd)
		if tc.wantMin == 0 {
			if got > 0.5 { //nolint:mnd
				t.Errorf(
					"%s: artistCreditFit(%q,%q) = %.2f, want low",
					tc.name,
					tc.local,
					tc.cnd,
					got,
				)
			}

			continue
		}

		if got < tc.wantMin {
			t.Errorf(
				"%s: artistCreditFit(%q,%q) = %.2f, want >= %.2f",
				tc.name,
				tc.local,
				tc.cnd,
				got,
				tc.wantMin,
			)
		}
	}
}

func TestEvidenceFactor(t *testing.T) {
	t.Parallel()

	cases := []struct {
		tracks int
		want   float64
	}{
		{0, evidenceFloor}, // no tracks — treated as minimum evidence
		{1, evidenceFloor}, // singleton — the harshest case
		{2, 0.925},         // halfway between floor and full
		{3, 1.0},           // full evidence
		{10, 1.0},          // large album — unscaled
	}

	for _, tc := range cases {
		got := evidenceFactor(tc.tracks)
		if diff := got - tc.want; diff < -0.001 || diff > 0.001 {
			t.Errorf("evidenceFactor(%d) = %.4f, want %.4f", tc.tracks, got, tc.want)
		}
	}
}
