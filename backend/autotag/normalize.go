package autotag

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// qualifierPattern matches common parenthesized / bracketed
// qualifiers that MB sometimes adds to titles but user tags often
// omit — e.g. `(Remastered 2009)`, `[Bonus Track]`, `(feat. X)`.
// Case-insensitive.  Only strips when the qualifier is at a
// word-boundary to avoid mangling titles like "Untitled (1)".
var qualifierPattern = regexp.MustCompile(
	`(?i)\s*[\(\[]\s*(` +
		`remaster(ed)?(\s+\d{4})?|` +
		`re-?master(ed)?(\s+\d{4})?|` +
		`\d{4}\s+remaster(ed)?|` +
		`deluxe(\s+edition)?|` +
		`expanded(\s+edition)?|` +
		`explicit|` +
		`clean|` +
		`bonus\s+track|` +
		`live(\s+at\s+[^\)\]]*)?|` +
		`acoustic|` +
		`radio\s+edit|` +
		`single\s+version|` +
		`album\s+version|` +
		`instrumental|` +
		`demo|` +
		`mono|` +
		`stereo|` +
		`feat\.?\s+[^\)\]]*|` +
		`featuring\s+[^\)\]]*|` +
		`ft\.?\s+[^\)\]]*` +
		`)\s*[\)\]]`,
)

// whitespaceCollapse replaces runs of whitespace with a single space.
var whitespaceCollapse = regexp.MustCompile(`\s+`)

// Normalize returns a comparison-friendly form of a title or
// artist-credit string:
//
//   - NFC unicode composition
//   - qualifier suffixes stripped (see qualifierPattern)
//   - all punctuation dropped
//   - case folded (lowercased)
//   - whitespace collapsed and trimmed
//
// The result is not intended to be human-readable — only used for
// equality and edit-distance comparisons inside the scorer.
func Normalize(s string) string {
	if s == "" {
		return ""
	}

	s = norm.NFC.String(s)
	s = qualifierPattern.ReplaceAllString(s, "")

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
