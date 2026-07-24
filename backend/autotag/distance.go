package autotag

import (
	"regexp"
	"strings"
	"unicode"
)

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

// sdPattern couples a regexp with the weight its removal carries in
// stringDist: when deleting the matched portion from both strings
// shrinks their distance, the recovered distance is re-added at
// `weight` instead of counting fully.  Weight 0 makes a difference
// in that portion free (known-cosmetic); higher weights make it
// cheap but not free.  Modeled on beets' SD_PATTERNS.
type sdPattern struct {
	re     *regexp.Regexp
	weight float64
}

// sdPatterns are applied in order; earlier patterns claim their
// portion of the string first.  Known qualifiers (whitelist) are
// free; generic parenthetical / bracketed content, featured-artist
// credits, leading articles, and part suffixes are de-weighted.
var sdPatterns = []sdPattern{
	{qualifierPattern, 0.0},
	{dashQualifierPattern, 0.0},
	{regexp.MustCompile(`^the `), 0.1},
	{regexp.MustCompile(`\b(featuring|feat\.?|ft\.?)[ :].*$`), 0.1},
	{regexp.MustCompile(`\(.*?\)`), 0.3},
	{regexp.MustCompile(`\[.*?\]`), 0.3},
	{regexp.MustCompile(`(, )?\b(pt\.|part) .+$`), 0.2},
}

// sdEndWords are articles that user tags sometimes rotate to the
// end with a comma: "Beatles, The" ≡ "The Beatles".
var sdEndWords = []string{"the", "a", "an"}

// stringDistBasic is the normalized edit distance between the two
// strings reduced to lowercase alphanumerics, in [0, 1].  Inputs
// are assumed to be lowercased and ASCII-folded already.
func stringDistBasic(a, b string) float64 {
	a = alnumOnly(a)
	b = alnumOnly(b)

	if a == "" && b == "" {
		return 0.0
	}

	longest := max(len(a), len(b))

	return float64(levenshtein(a, b)) / float64(longest)
}

// alnumOnly strips everything but letters and digits.
func alnumOnly(s string) string {
	var b strings.Builder

	b.Grow(len(s))

	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}

	return b.String()
}

// stringDist returns an "intuitive" distance between two titles or
// artist credits, in [0, 1].  It is a normalized edit distance with
// tweaks reflecting how music metadata actually differs (ported
// from beets' string_dist):
//
//   - accents transliterated, case ignored
//   - "X, The" rotated back to "The X" (same for "A"/"An")
//   - "&" ≡ "and"
//   - known qualifier suffixes ("(Remastered 2009)", "- Radio Edit")
//     are free; unknown parenthesized/bracketed content, featured-
//     artist credits, leading articles, and "Part N" suffixes are
//     de-weighted rather than counting as full edits
func stringDist(a, b string) float64 {
	a = strings.ToLower(asciiFold(a))
	b = strings.ToLower(asciiFold(b))

	a = rotateEndWord(a)
	b = rotateEndWord(b)

	a = strings.ReplaceAll(a, "&", " and ")
	b = strings.ReplaceAll(b, "&", " and ")

	base := stringDistBasic(a, b)
	penalty := 0.0

	for _, p := range sdPatterns {
		ca := p.re.ReplaceAllString(a, "")
		cb := p.re.ReplaceAllString(b, "")

		if ca == a && cb == b {
			continue
		}

		// The pattern was present: measure how much of the distance
		// it accounted for and re-add that share at reduced weight.
		caseDist := stringDistBasic(ca, cb)

		delta := base - caseDist
		if delta <= 0 {
			continue
		}

		a, b = ca, cb
		base = caseDist
		penalty += p.weight * delta
	}

	return base + penalty
}

// rotateEndWord undoes sort-style article rotation: "beatles, the"
// → "the beatles".  Input must be lowercased.
func rotateEndWord(s string) string {
	for _, w := range sdEndWords {
		suffix := ", " + w
		if strings.HasSuffix(s, suffix) {
			return w + " " + s[:len(s)-len(suffix)]
		}
	}

	return s
}

// titleSimilarity returns a score in [0, 1] from stringDist.  1.0
// means identical after normalization, 0.0 means fully dissimilar.
func titleSimilarity(a, b string) float64 {
	if strings.TrimSpace(a) == "" && strings.TrimSpace(b) == "" {
		return 1.0
	}

	sim := 1.0 - stringDist(a, b)
	if sim < 0 {
		return 0.0
	}

	return sim
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
	// hides also doesn't count against the score.  MB recording
	// lengths routinely differ from file durations by a few seconds
	// (encoder padding, different masters), so the grace band is
	// deliberately wider than perceptual accuracy; Picard tolerates
	// up to 30 s linearly and beets grants a flat 10 s grace.  Past
	// the threshold the penalty scales with delta / candidateMs
	// (i.e. percentage of candidate-track length): a 5 s delta on a
	// 4 min track is small, the same delta on a 30 s interlude is
	// huge.  At lengthFullyWrongPct of candidate length the score
	// hits zero; beyond that it stays clamped to zero.
	lengthExactMs       int64   = 5000
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

// trackNumberOK reports whether the local track number agrees with
// the candidate position (0 = unknown, never a match).
func trackNumberOK(local LocalTrack, cand CandidateTrack) bool {
	return local.TrackNumber > 0 && local.TrackNumber == cand.Position
}

// combineTrackScore folds the per-track components into one score.
// Split out so AlignTracks can compute the components once per pair
// and still share the exact formula with trackDistance.
func combineTrackScore(title, length float64, numberOK bool) float64 {
	var trackOK float64
	if numberOK {
		trackOK = 1.0
	}

	return title*weightTitle + length*weightLength + trackOK*weightTrackNumber
}

// trackDistance scores how well one local track aligns with one
// candidate track.  Higher is better.  Caller decides what to do
// with the result — this function has no threshold.
func trackDistance(local LocalTrack, cand CandidateTrack) float64 {
	return combineTrackScore(
		titleSimilarity(local.Title, cand.Title),
		lengthScore(local.LengthMillis, cand.LengthMillis),
		trackNumberOK(local, cand),
	)
}
