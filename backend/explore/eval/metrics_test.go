package eval

import (
	"math"
	"strings"
	"testing"
)

// rankerFromIDs builds a Ranker that returns a fixed ordering keyed by
// query, for deterministic metric tests.
func rankerFromIDs(table map[string][]Result) Ranker {
	return RankerFunc(func(query string, limit int) []Result {
		out := table[query]
		if limit < len(out) {
			out = out[:limit]
		}

		return out
	})
}

func approx(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestReciprocalRank(t *testing.T) {
	tests := []struct {
		name    string
		ranked  []Result
		expect  []Expected
		wantRR  float64
		wantPos int
	}{
		{
			name:    "top result",
			ranked:  []Result{{MBID: "a"}, {MBID: "b"}},
			expect:  []Expected{{MBID: "a"}},
			wantRR:  1.0,
			wantPos: 1,
		},
		{
			name:    "third result",
			ranked:  []Result{{MBID: "x"}, {MBID: "y"}, {MBID: "a"}},
			expect:  []Expected{{MBID: "a"}},
			wantRR:  1.0 / 3.0,
			wantPos: 3,
		},
		{
			name:    "absent",
			ranked:  []Result{{MBID: "x"}, {MBID: "y"}},
			expect:  []Expected{{MBID: "a"}},
			wantRR:  0,
			wantPos: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := Fixture{Query: "q", Expect: tt.expect}
			got := scoreQuery(fx, tt.ranked, 5)

			if !approx(got.ReciprocalRank, tt.wantRR) {
				t.Errorf("RR = %v, want %v", got.ReciprocalRank, tt.wantRR)
			}

			if got.BestRank != tt.wantPos {
				t.Errorf("BestRank = %d, want %d", got.BestRank, tt.wantPos)
			}
		})
	}
}

func TestPrecisionAtK(t *testing.T) {
	fx := Fixture{
		Query:  "q",
		Expect: []Expected{{MBID: "a"}, {MBID: "b"}},
	}
	ranked := []Result{{MBID: "a"}, {MBID: "x"}, {MBID: "b"}, {MBID: "y"}}

	got := scoreQuery(fx, ranked, 4)

	// 2 relevant out of k=4.
	if !approx(got.PrecisionAtK, 0.5) {
		t.Errorf("P@4 = %v, want 0.5", got.PrecisionAtK)
	}
}

func TestNDCGRespectsOrdering(t *testing.T) {
	expect := []Expected{{MBID: "a", Grade: 3}, {MBID: "b", Grade: 1}}

	// Ideal ordering: high-grade result first.
	good := scoreQuery(
		Fixture{Query: "q", Expect: expect},
		[]Result{{MBID: "a"}, {MBID: "b"}, {MBID: "z"}},
		5,
	)

	// Worse ordering: high-grade result buried below an irrelevant one.
	bad := scoreQuery(
		Fixture{Query: "q", Expect: expect},
		[]Result{{MBID: "z"}, {MBID: "b"}, {MBID: "a"}},
		5,
	)

	if !approx(good.NDCGAtK, 1.0) {
		t.Errorf("ideal ordering nDCG = %v, want 1.0", good.NDCGAtK)
	}

	if bad.NDCGAtK >= good.NDCGAtK {
		t.Errorf("worse ordering nDCG %v should be < ideal %v", bad.NDCGAtK, good.NDCGAtK)
	}
}

func TestTypeMustMatchWhenPinned(t *testing.T) {
	fx := Fixture{
		Query:  "q",
		Expect: []Expected{{Type: "artist", MBID: "a"}},
	}

	// Same MBID but wrong entity type — must not count.
	wrongType := scoreQuery(fx, []Result{{EntityType: "recording", MBID: "a"}}, 5)
	if wrongType.Hit() {
		t.Error("result with wrong entity type counted as a hit")
	}

	rightType := scoreQuery(fx, []Result{{EntityType: "artist", MBID: "a"}}, 5)
	if !rightType.Hit() {
		t.Error("result with matching entity type did not count")
	}
}

func TestEvaluateAggregates(t *testing.T) {
	fixtures := []Fixture{
		{Query: "hit-top", Expect: []Expected{{MBID: "a"}}},
		{Query: "hit-second", Expect: []Expected{{MBID: "a"}}},
		{Query: "miss", Expect: []Expected{{MBID: "a"}}},
	}

	r := rankerFromIDs(map[string][]Result{
		"hit-top":    {{MBID: "a"}},
		"hit-second": {{MBID: "x"}, {MBID: "a"}},
		"miss":       {{MBID: "x"}, {MBID: "y"}},
	})

	report := Evaluate(r, fixtures, 5)

	// MRR = (1 + 1/2 + 0) / 3.
	wantMRR := (1.0 + 0.5 + 0.0) / 3.0
	if !approx(report.MRR, wantMRR) {
		t.Errorf("MRR = %v, want %v", report.MRR, wantMRR)
	}

	// 2 of 3 queries surfaced the result somewhere in topK.
	if !approx(report.HitRate, 2.0/3.0) {
		t.Errorf("HitRate = %v, want %v", report.HitRate, 2.0/3.0)
	}

	// Only 1 of 3 had it at rank 1.
	if !approx(report.Top1Rate, 1.0/3.0) {
		t.Errorf("Top1Rate = %v, want %v", report.Top1Rate, 1.0/3.0)
	}

	if len(report.Misses()) != 2 {
		t.Errorf("Misses = %d, want 2", len(report.Misses()))
	}
}

func TestParseFixtures(t *testing.T) {
	const doc = `[
		{"query": "radiohead", "expect": [{"type": "artist", "mbid": "abc"}]},
		{"query": "ok computer", "note": "album not band", "expect": [{"mbid": "def", "grade": 2}]}
	]`

	fixtures, err := ParseFixtures(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("ParseFixtures: %v", err)
	}

	if len(fixtures) != 2 {
		t.Fatalf("got %d fixtures, want 2", len(fixtures))
	}

	if fixtures[0].Expect[0].MBID != "abc" {
		t.Errorf("MBID = %q, want abc", fixtures[0].Expect[0].MBID)
	}
}

func TestParseFixturesEmpty(t *testing.T) {
	_, err := ParseFixtures(strings.NewReader(`[]`))
	if err == nil {
		t.Fatal("expected ErrNoFixtures, got nil")
	}
}
