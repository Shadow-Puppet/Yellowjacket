package eval

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// QueryScore holds the per-query metrics for one fixture.
type QueryScore struct {
	Query string
	Note  string

	// BestRank is the 1-based rank of the highest-placed expected
	// result, or 0 if none of the expected results appear in topK.
	BestRank int

	ReciprocalRank float64
	PrecisionAtK   float64
	NDCGAtK        float64
}

// Hit reports whether any expected result landed in topK.
func (q QueryScore) Hit() bool {
	return q.BestRank > 0
}

// Report aggregates per-query scores into the numbers you watch across
// a ranking change: mean reciprocal rank, mean precision@k, mean
// nDCG@k, plus the list of queries that missed entirely.
type Report struct {
	K          int
	NumQueries int

	MRR      float64
	MeanPAtK float64
	MeanNDCG float64
	HitRate  float64 // fraction of queries with any expected result in topK
	Top1Rate float64 // fraction whose best expected result is rank 1
	PerQuery []QueryScore
}

// Evaluate runs every fixture through the ranker and aggregates the
// results into a Report.  topK bounds how deep a result can be and
// still count (a result at rank 20 helps no one).
func Evaluate(r Ranker, fixtures []Fixture, topK int) Report {
	if topK <= 0 {
		topK = 5
	}

	report := Report{K: topK, NumQueries: len(fixtures)}

	for _, fx := range fixtures {
		ranked := r.Rank(fx.Query, topK)
		report.PerQuery = append(report.PerQuery, scoreQuery(fx, ranked, topK))
	}

	for _, q := range report.PerQuery {
		report.MRR += q.ReciprocalRank
		report.MeanPAtK += q.PrecisionAtK
		report.MeanNDCG += q.NDCGAtK

		if q.Hit() {
			report.HitRate++
		}

		if q.BestRank == 1 {
			report.Top1Rate++
		}
	}

	if n := float64(len(fixtures)); n > 0 {
		report.MRR /= n
		report.MeanPAtK /= n
		report.MeanNDCG /= n
		report.HitRate /= n
		report.Top1Rate /= n
	}

	return report
}

// scoreQuery computes the metrics for a single fixture against a ranked
// result list.
func scoreQuery(fx Fixture, ranked []Result, topK int) QueryScore {
	score := QueryScore{Query: fx.Query, Note: fx.Note}

	limit := min(topK, len(ranked))

	relevantInK := 0

	for i := range limit {
		if !anyMatch(fx.Expect, ranked[i]) {
			continue
		}

		relevantInK++

		if score.BestRank == 0 {
			score.BestRank = i + 1
			score.ReciprocalRank = 1.0 / float64(i+1)
		}
	}

	score.PrecisionAtK = float64(relevantInK) / float64(topK)
	score.NDCGAtK = ndcg(fx.Expect, ranked, topK)

	return score
}

// anyMatch reports whether a result satisfies any expectation.
func anyMatch(expected []Expected, r Result) bool {
	for _, e := range expected {
		if e.matches(r) {
			return true
		}
	}

	return false
}

// ndcg computes normalized discounted cumulative gain at k using graded
// relevance.  Returns 0 when there are no expected results.
func ndcg(expected []Expected, ranked []Result, k int) float64 {
	ideal := idealDCG(expected, k)
	if ideal == 0 {
		return 0
	}

	limit := min(k, len(ranked))

	dcg := 0.0

	for i := range limit {
		g := matchedGrade(expected, ranked[i])
		if g == 0 {
			continue
		}

		dcg += gain(g, i)
	}

	return dcg / ideal
}

// matchedGrade returns the relevance grade for a result, or 0 if it
// matches no expectation.
func matchedGrade(expected []Expected, r Result) int {
	for _, e := range expected {
		if e.matches(r) {
			return e.grade()
		}
	}

	return 0
}

// idealDCG is the DCG of the best possible ordering: every expected
// result, sorted by grade descending, placed at the front.
func idealDCG(expected []Expected, k int) float64 {
	grades := make([]int, 0, len(expected))
	for _, e := range expected {
		grades = append(grades, e.grade())
	}

	sort.Sort(sort.Reverse(sort.IntSlice(grades)))

	limit := min(k, len(grades))

	ideal := 0.0
	for i := range limit {
		ideal += gain(grades[i], i)
	}

	return ideal
}

// gain is the discounted gain of a grade at 0-based position i.
func gain(grade, i int) float64 {
	return (math.Pow(2, float64(grade)) - 1) / math.Log2(float64(i+2))
}

// Format renders a Report as a human-readable table for test output.
func (r Report) Format() string {
	var b strings.Builder

	fmt.Fprintf(&b, "ranking eval — %d queries @k=%d\n", r.NumQueries, r.K)
	fmt.Fprintf(&b, "  MRR        %.3f\n", r.MRR)
	fmt.Fprintf(&b, "  P@%d        %.3f\n", r.K, r.MeanPAtK)
	fmt.Fprintf(&b, "  nDCG@%d     %.3f\n", r.K, r.MeanNDCG)
	fmt.Fprintf(&b, "  hit rate   %.3f\n", r.HitRate)
	fmt.Fprintf(&b, "  top-1 rate %.3f\n", r.Top1Rate)

	misses := r.Misses()
	if len(misses) > 0 {
		b.WriteString("  misses:\n")

		for _, m := range misses {
			fmt.Fprintf(&b, "    %-40q rank=%s\n", m.Query, rankLabel(m.BestRank))
		}
	}

	return b.String()
}

// Misses returns the queries whose best expected result was absent
// from topK or buried below rank 1 — the regression watch-list.
func (r Report) Misses() []QueryScore {
	var out []QueryScore

	for _, q := range r.PerQuery {
		if q.BestRank != 1 {
			out = append(out, q)
		}
	}

	return out
}

func rankLabel(rank int) string {
	if rank == 0 {
		return "absent"
	}

	return strconv.Itoa(rank)
}
