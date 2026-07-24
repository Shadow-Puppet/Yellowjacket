package autotag

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// qualifierAlternation is the regex alternation of parenthesized /
// dash-suffixed qualifiers that MB sometimes adds to titles but user
// tags often omit — e.g. `Remastered 2009`, `Bonus Track`, `feat. X`.
// Shared between the parenthesized and dash-suffix qualifier patterns.
const qualifierAlternation = `remaster(ed)?(\s+\d{4})?|` +
	`re-?master(ed)?(\s+\d{4})?|` +
	`\d{4}\s+remaster(ed)?|` +
	`deluxe(\s+(edition|version))?|` +
	`expanded(\s+(edition|version))?|` +
	`anniversary(\s+(edition|version))?|` +
	`explicit|` +
	`clean|` +
	`bonus\s+track|` +
	`live(\s+at\s+[^\)\]]*)?|` +
	`acoustic|` +
	`radio\s+edit|` +
	`single\s+version|` +
	`album\s+version|` +
	`original\s+mix|` +
	`instrumental|` +
	`demo|` +
	`mono|` +
	`stereo|` +
	`feat\.?\s+[^\)\]]*|` +
	`featuring\s+[^\)\]]*|` +
	`ft\.?\s+[^\)\]]*`

// qualifierPattern matches common parenthesized / bracketed
// qualifiers.  Case-insensitive.  Only strips when the qualifier is
// at a word-boundary to avoid mangling titles like "Untitled (1)".
var qualifierPattern = regexp.MustCompile(
	`(?i)\s*[\(\[]\s*(` + qualifierAlternation + `)\s*[\)\]]`,
)

// dashQualifierPattern matches the dash-suffix form of the same
// qualifiers — `Song - 2009 Remaster`, `Song – Radio Edit` — which
// streaming-service-derived tags use instead of parentheses.
var dashQualifierPattern = regexp.MustCompile(
	`(?i)\s+[-–—]\s+(` + qualifierAlternation + `)\s*$`,
)

// whitespaceCollapse replaces runs of whitespace with a single space.
var whitespaceCollapse = regexp.MustCompile(`\s+`)

// asciiSpecials maps letters that unicode decomposition alone can't
// reduce to ASCII (they aren't combining-mark compositions).
var asciiSpecials = strings.NewReplacer(
	"ß", "ss", "ẞ", "SS",
	"æ", "ae", "Æ", "AE",
	"œ", "oe", "Œ", "OE",
	"ø", "o", "Ø", "O",
	"đ", "d", "Đ", "D",
	"ð", "d", "Ð", "D",
	"þ", "th", "Þ", "Th",
	"ł", "l", "Ł", "L",
	"ı", "i",
)

// asciiFold transliterates accented characters to their closest
// ASCII equivalent ("Beyoncé" → "Beyonce", "Björk" → "Bjork") so
// diacritic differences between user tags and MB data don't count
// as edits.  Non-Latin scripts pass through unchanged.
func asciiFold(s string) string {
	// Fast path: nothing to fold in pure-ASCII strings.
	ascii := true

	for i := range len(s) {
		if s[i] >= 0x80 {
			ascii = false

			break
		}
	}

	if ascii {
		return s
	}

	s = asciiSpecials.Replace(s)

	// NFKD splits accented letters into base + combining marks;
	// dropping the marks (unicode.Mn) leaves the base letter.  The
	// chain is stateful, so build it per call — it's cheap and this
	// keeps concurrent scorers safe.
	t := transform.Chain(norm.NFKD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

	folded, _, err := transform.String(t, s)
	if err != nil {
		return s
	}

	return folded
}

// Normalize returns a comparison-friendly form of a title or
// artist-credit string:
//
//   - ASCII transliteration (accents folded)
//   - qualifier suffixes stripped (see qualifierPattern)
//   - "&" replaced with "and"
//   - all remaining punctuation dropped
//   - case folded (lowercased)
//   - whitespace collapsed and trimmed
//
// The result is not intended to be human-readable — only used for
// equality comparisons (local candidate matching) and search query
// building.  Fuzzy comparisons go through titleSimilarity, which
// keeps more structure.
func Normalize(s string) string {
	if s == "" {
		return ""
	}

	s = asciiFold(s)
	s = qualifierPattern.ReplaceAllString(s, "")
	s = dashQualifierPattern.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&", " and ")

	var b strings.Builder

	b.Grow(len(s))

	for _, r := range s {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		}
	}

	return strings.TrimSpace(whitespaceCollapse.ReplaceAllString(b.String(), " "))
}
