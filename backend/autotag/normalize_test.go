package autotag_test

import (
	"testing"

	"yellowjacket/backend/autotag"
)

func TestNormalize(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in   string
		want string
	}{
		"empty":              {"", ""},
		"already normalized": {"abbey road", "abbey road"},
		"case fold":          {"Abbey Road", "abbey road"},
		"punctuation stripped": {
			"Sgt. Pepper's Lonely Hearts Club Band!",
			"sgt peppers lonely hearts club band",
		},
		"accents fold to ascii":      {"Beyoncé", "beyonce"},
		"eszett folds":               {"Motörhead & Björk", "motorhead and bjork"},
		"ampersand becomes and":      {"Simon & Garfunkel", "simon and garfunkel"},
		"remastered qualifier":       {"Abbey Road (Remastered 2009)", "abbey road"},
		"remaster no year":           {"Abbey Road (Remaster)", "abbey road"},
		"explicit qualifier":         {"Lemonade [Explicit]", "lemonade"},
		"feat qualifier":             {"Yellow (feat. Coldplay)", "yellow"},
		"bonus track qualifier":      {"Hey Jude [Bonus Track]", "hey jude"},
		"dash suffix qualifier":      {"Hey Jude - 2015 Remaster", "hey jude"},
		"dash radio edit":            {"One More Time - Radio Edit", "one more time"},
		"collapse whitespace":        {"  Abbey    Road  ", "abbey road"},
		"non-ascii digits kept":      {"Track 7", "track 7"},
		"numbered title not mangled": {"Untitled (1)", "untitled 1"},
		"year remaster form":         {"Abbey Road (2009 Remaster)", "abbey road"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := autotag.Normalize(tc.in)
			if got != tc.want {
				t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
