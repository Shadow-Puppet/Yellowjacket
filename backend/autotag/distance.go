package autotag

// levenshtein returns the Levenshtein edit distance between a and
// b, operating on runes so multi-byte characters count as one edit.
// Allocates a single O(min(len)) scratch slice.
func levenshtein(a, b string) int {
	ra := []rune(a)
	rb := []rune(b)

	if len(ra) == 0 {
		return len(rb)
	}

	if len(rb) == 0 {
		return len(ra)
	}

	if len(ra) > len(rb) {
		ra, rb = rb, ra
	}

	prev := make([]int, len(ra)+1)
	for i := range prev {
		prev[i] = i
	}

	for j := 1; j <= len(rb); j++ {
		curr0 := prev[0]
		prev[0] = j

		for i := 1; i <= len(ra); i++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}

			newVal := min3(
				prev[i]+1,   // deletion
				prev[i-1]+1, // insertion
				curr0+cost,  // substitution
			)
			curr0 = prev[i]
			prev[i] = newVal
		}
	}

	return prev[len(ra)]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}

		return c
	}

	if b < c {
		return b
	}

	return c
}

// titleSimilarity returns a score in [0, 1] from normalized edit
// distance.  1.0 means identical after normalization, 0.0 means
// fully dissimilar.  Both sides are normalized inside.
func titleSimilarity(a, b string) float64 {
	na := Normalize(a)
	nb := Normalize(b)

	if na == "" && nb == "" {
		return 1.0
	}

	longest := len(na)
	if len(nb) > longest {
		longest = len(nb)
	}

	if longest == 0 {
		return 0.0
	}

	dist := levenshtein(na, nb)

	return 1.0 - float64(dist)/float64(longest)
}

// Scoring weights for the per-track distance function. Local reads
// only — never written at runtime, so no mutex.  Values stay small
// so a future tuning pass can nudge them without rescaling.
const (
	weightTitle       = 0.60
	weightLength      = 0.30
	weightTrackNumber = 0.10

	// Length deltas at or below lengthExactMs score 1.0 — matches
	// the frontend's "subtle drift" threshold so anything the UI
	// hides also doesn't count against the score.  Past the
	// threshold the penalty scales with delta / candidateMs (i.e.
	// percentage of candidate-track length): a 5 s delta on a
	// 4 min track is small, the same delta on a 30 s interlude is
	// huge.  At lengthFullyWrongPct of candidate length the score
	// hits zero; beyond that it stays clamped to zero.
	lengthExactMs       int64   = 2000
	lengthFullyWrongPct float64 = 0.20

	// A title below titleReject has too little signal for this
	// alignment to count as matched.
	titleReject = 0.60
)

// lengthScore returns 1.0 for deltas <= lengthExactMs, 0.0 for
// deltas >= lengthFullyWrongPct of the candidate length, linear
// in delta-as-percentage-of-candidate-length between.  When
// either side is zero (unknown), returns 0.5 so length is treated
// as neutral.
func lengthScore(localMs, candidateMs int64) float64 {
	if localMs <= 0 || candidateMs <= 0 {
		return 0.5
	}

	delta := localMs - candidateMs
	if delta < 0 {
		delta = -delta
	}

	if delta <= lengthExactMs {
		return 1.0
	}

	pct := float64(delta) / float64(candidateMs)
	if pct >= lengthFullyWrongPct {
		return 0.0
	}

	return 1.0 - pct/lengthFullyWrongPct
}

// trackDistance scores how well one local track aligns with one
// candidate track.  Higher is better.  Caller decides what to do
// with the result — this function has no threshold.
func trackDistance(local LocalTrack, cand CandidateTrack) float64 {
	title := titleSimilarity(local.Title, cand.Title)
	length := lengthScore(local.LengthMillis, cand.LengthMillis)

	var trackOK float64
	if local.TrackNumber > 0 && local.TrackNumber == cand.Position {
		trackOK = 1.0
	}

	return title*weightTitle + length*weightLength + trackOK*weightTrackNumber
}
