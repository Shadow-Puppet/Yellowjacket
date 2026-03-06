package metadata

import (
	"testing"
)

func TestParseGenres(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{
			name: "single genre",
			raw:  "Rock",
			want: []string{"Rock"},
		},
		{
			name: "semicolon separated",
			raw:  "Rock; Electronic",
			want: []string{"Rock", "Electronic"},
		},
		{
			name: "comma separated",
			raw:  "Rock, Jazz",
			want: []string{"Rock", "Jazz"},
		},
		{
			name: "mixed separators",
			raw:  "Rock; Pop, Jazz",
			want: []string{"Rock", "Pop", "Jazz"},
		},
		{
			name: "case normalization deduplicates",
			raw:  "rock,ROCK,Rock",
			want: []string{"Rock"},
		},
		{
			name: "whitespace and empty segments",
			raw:  "  Pop ; ; Jazz , ",
			want: []string{"Pop", "Jazz"},
		},
		{
			name: "empty string",
			raw:  "",
			want: nil,
		},
		{
			name: "only separators",
			raw:  ";;,,;,",
			want: nil,
		},
		{
			name: "whitespace only",
			raw:  "   ",
			want: nil,
		},
		{
			name: "title case multi-word genre",
			raw:  "hip hop; drum and bass",
			want: []string{"Hip Hop", "Drum And Bass"},
		},
		{
			name: "preserves already correct casing",
			raw:  "Post-Punk",
			want: []string{"Post-Punk"},
		},
		{
			name: "duplicate after title case",
			raw:  "electronic; Electronic; ELECTRONIC",
			want: []string{"Electronic"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ParseGenres(tt.raw)
			if !slicesEqual(got, tt.want) {
				t.Errorf(
					"ParseGenres(%q) = %v, want %v",
					tt.raw, got, tt.want,
				)
			}
		})
	}
}

// slicesEqual reports whether two string slices are equal.
func slicesEqual(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}

	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
