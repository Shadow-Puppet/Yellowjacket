package explore

import "testing"

func TestDiceCoefficientTypoTolerance(t *testing.T) {
	tests := []struct {
		name    string
		a, b    string
		wantMin float64 // score must be at least this
		wantMax float64 // ...and at most this
	}{
		{name: "identical", a: "beatles", b: "beatles", wantMin: 1.0, wantMax: 1.0},
		{name: "single typo", a: "beetles", b: "beatles", wantMin: 0.5, wantMax: 0.9},
		{name: "missing char", a: "nirvna", b: "nirvana", wantMin: 0.5, wantMax: 0.95},
		// Longer-name typo: a single substitution stays comfortably above
		// the rescue threshold, which is the realistic case (artist and
		// album names are rarely as short as five characters).
		{name: "longer name typo", a: "metalica", b: "metallica", wantMin: 0.5, wantMax: 0.95},
		// Adjacent transposition in a short word is bigrams' known weak
		// spot — it breaks most windows, so it scores below threshold.
		// Documented, not a bug: bigram overlap targets substitution,
		// insertion, and deletion typos.
		{name: "short transposition", a: "raido", b: "radio", wantMin: 0.0, wantMax: 0.34},
		{name: "case and space", a: "The Beatles", b: "the beatles", wantMin: 1.0, wantMax: 1.0},
		{name: "unrelated", a: "beatles", b: "metallica", wantMin: 0.0, wantMax: 0.34},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := diceCoefficient(fuzzyBigrams(tc.a), fuzzyBigrams(tc.b))
			if got < tc.wantMin || got > tc.wantMax {
				t.Errorf("diceCoefficient(%q, %q) = %.3f, want in [%.2f, %.2f]",
					tc.a, tc.b, got, tc.wantMin, tc.wantMax)
			}
		})
	}
}

func TestFuzzyBigramsShortInput(t *testing.T) {
	if got := fuzzyBigrams("a"); got != nil {
		t.Errorf("fuzzyBigrams(%q) = %v, want nil", "a", got)
	}
}
