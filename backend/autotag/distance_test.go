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
		{"Hey Jude", "Hey Jude (Remastered 2009)", 1.00}, // qualifier stripped
		{"Hey Jude", "HEY JUDE!", 1.00},                  // case + punct
		{"Hey Jude", "Hay Jude", 0.85},                   // one char off
		{"Hey Jude", "Let It Be", 0.00},                  // different
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
		{200000, 200500, 1.0}, // 0.5s — under 2s threshold
		{200000, 201900, 1.0}, // 1.9s — under 2s threshold
		{200000, 202000, 1.0}, // exactly 2s — still full credit
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
