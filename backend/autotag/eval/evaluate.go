package eval

import "fmt"

// ScoredCandidate is one ranked candidate reduced to what the harness
// checks: its MBID and final score.  The slice a Ranker returns must
// be ordered best-first.
type ScoredCandidate struct {
	MBID  string
	Score float64
}

// Ranker scores and orders the candidates of a single case.  Best
// candidate first.  Implemented by adapting the real scorer (see
// eval_harness_test.go), which is why the eval package itself never
// imports autotag.
type Ranker interface {
	Rank(c Case) []ScoredCandidate
}

// RankerFunc adapts a plain function to the Ranker interface.
type RankerFunc func(c Case) []ScoredCandidate

// Rank calls the underlying function.
func (f RankerFunc) Rank(c Case) []ScoredCandidate {
	return f(c)
}

// CaseResult records how one case fared.  Failures is empty when the
// case passed every pinned expectation.
type CaseResult struct {
	Case     Case
	Failures []string
}

// Passed reports whether the case met every expectation.
func (r CaseResult) Passed() bool {
	return len(r.Failures) == 0
}

// Report aggregates results across a case set.
type Report struct {
	Results []CaseResult
}

// Passed counts cases that met every expectation.
func (r Report) Passed() int {
	n := 0

	for _, c := range r.Results {
		if c.Passed() {
			n++
		}
	}

	return n
}

// Accuracy is the fraction of cases that passed, in [0, 1].
func (r Report) Accuracy() float64 {
	if len(r.Results) == 0 {
		return 0
	}

	return float64(r.Passed()) / float64(len(r.Results))
}

// Evaluate runs every case through the ranker and checks its pinned
// expectations (ExpectTop, MaxScore ceilings, MinScore floors),
// returning a Report the caller can assert on and print.
func Evaluate(cases []Case, ranker Ranker) Report {
	rep := Report{Results: make([]CaseResult, 0, len(cases))}

	for _, c := range cases {
		rep.Results = append(rep.Results, evaluateCase(c, ranker))
	}

	return rep
}

func evaluateCase(c Case, ranker Ranker) CaseResult {
	ranked := ranker.Rank(c)

	res := CaseResult{Case: c}

	byMBID := make(map[string]float64, len(ranked))
	for _, r := range ranked {
		byMBID[r.MBID] = r.Score
	}

	if c.ExpectTop != "" {
		switch {
		case len(ranked) == 0:
			res.Failures = append(
				res.Failures,
				"expected top "+c.ExpectTop+" but ranking was empty",
			)
		case ranked[0].MBID != c.ExpectTop:
			res.Failures = append(res.Failures, fmt.Sprintf(
				"top = %q (%.3f), want %q (%.3f)",
				ranked[0].MBID, ranked[0].Score, c.ExpectTop, byMBID[c.ExpectTop],
			))
		}
	}

	for mbid, ceil := range c.MaxScore {
		if got, ok := byMBID[mbid]; ok && got > ceil {
			res.Failures = append(res.Failures, fmt.Sprintf(
				"%s scored %.3f, want <= %.3f", mbid, got, ceil,
			))
		}
	}

	for mbid, floor := range c.MinScore {
		if got, ok := byMBID[mbid]; ok && got < floor {
			res.Failures = append(res.Failures, fmt.Sprintf(
				"%s scored %.3f, want >= %.3f", mbid, got, floor,
			))
		}
	}

	return res
}
