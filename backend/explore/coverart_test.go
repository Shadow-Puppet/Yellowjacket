package explore_test

import (
	"testing"

	"yellowjacket/backend/explore"
)

func TestCoverArtURL(t *testing.T) {
	t.Parallel()

	mbid := "76df3287-6cda-33eb-8e9a-044b5e15c37c"

	got := explore.CoverArtURL(mbid)
	want := "https://coverartarchive.org/release/76df3287-6cda-33eb-8e9a-044b5e15c37c/front-250"

	if got != want {
		t.Errorf("CoverArtURL(%q) = %q, want %q", mbid, got, want)
	}
}

func TestCoverArtURLSize(t *testing.T) {
	t.Parallel()

	mbid := "76df3287-6cda-33eb-8e9a-044b5e15c37c"

	tests := []struct {
		size int
		want string
	}{
		{
			250,
			"https://coverartarchive.org/release/76df3287-6cda-33eb-8e9a-044b5e15c37c/front-250",
		},
		{
			500,
			"https://coverartarchive.org/release/76df3287-6cda-33eb-8e9a-044b5e15c37c/front-500",
		},
		{
			1200,
			"https://coverartarchive.org/release/76df3287-6cda-33eb-8e9a-044b5e15c37c/front-1200",
		},
	}

	for _, tt := range tests {
		got := explore.CoverArtURLSize(mbid, tt.size)
		if got != tt.want {
			t.Errorf("CoverArtURLSize(%q, %d) = %q, want %q",
				mbid, tt.size, got, tt.want)
		}
	}
}
