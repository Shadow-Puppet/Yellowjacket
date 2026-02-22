// Package metadata provides audio file metadata extraction utilities.
package metadata

import (
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// genreSeparators defines the characters treated as genre delimiters.
const genreSeparators = ",;"

// ParseGenres splits a raw genre string on commas and semicolons,
// trims whitespace, normalizes each entry to title case, removes
// duplicates, and returns the unique genre names.  An empty or
// whitespace-only input returns nil.
func ParseGenres(raw string) []string {
	parts := strings.FieldsFunc(
		raw, func(r rune) bool {
			return strings.ContainsRune(genreSeparators, r)
		},
	)

	caser := cases.Title(language.English)
	seen := make(map[string]struct{}, len(parts))

	var genres []string

	for _, p := range parts {
		name := strings.TrimSpace(p)
		if name == "" {
			continue
		}

		name = caser.String(name)

		if _, ok := seen[name]; ok {
			continue
		}

		seen[name] = struct{}{}

		genres = append(genres, name)
	}

	return genres
}
