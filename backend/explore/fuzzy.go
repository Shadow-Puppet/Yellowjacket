package explore

import "strings"

// Character-bigram similarity, used by the search index's typo-tolerant
// rescue pass (see fuzzyRescue).  A name is represented as the set of its
// sliding 2-rune windows; a single-character typo alters only the one or
// two bigrams that span it, so most of the set survives.  Overlap between
// two such sets (Dice coefficient) therefore stays high across a
// misspelling, where prefix matching collapses to nothing.

// fuzzyNormalize lowercases and collapses runs of whitespace to a single
// space so bigram sets are stable across casing and spacing noise.
func fuzzyNormalize(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// fuzzyBigrams returns the set of character bigrams of s after
// normalization.  Returns nil for inputs shorter than two runes, which
// have no bigram and can't be scored.
func fuzzyBigrams(s string) map[string]struct{} {
	runes := []rune(fuzzyNormalize(s))
	if len(runes) < 2 {
		return nil
	}

	set := make(map[string]struct{}, len(runes))

	for i := 0; i+1 < len(runes); i++ {
		set[string(runes[i:i+2])] = struct{}{}
	}

	return set
}

// diceCoefficient is 2·|A∩B| / (|A|+|B|), a similarity in [0, 1] where 1
// is an identical bigram set and 0 is disjoint.  Iterating the smaller
// set keeps the intersection count cheap.
func diceCoefficient(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}

	small, large := a, b
	if len(large) < len(small) {
		small, large = large, small
	}

	intersection := 0

	for bg := range small {
		if _, ok := large[bg]; ok {
			intersection++
		}
	}

	return 2 * float64(intersection) / float64(len(a)+len(b))
}
