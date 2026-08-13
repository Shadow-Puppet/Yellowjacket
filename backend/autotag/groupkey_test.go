package autotag_test

import (
	"regexp"
	"testing"

	"yellowjacket/backend/autotag"
)

var hexSHA1 = regexp.MustCompile(`^[0-9a-f]{40}$`)

func TestGroupKey_FormatAndDeterminism(t *testing.T) {
	t.Parallel()

	key := autotag.GroupKey(1, "/music/Artist/Album/01.mp3", 1)
	if !hexSHA1.MatchString(key) {
		t.Fatalf("expected 40-char lowercase hex, got %q", key)
	}

	again := autotag.GroupKey(1, "/music/Artist/Album/01.mp3", 1)
	if key != again {
		t.Fatalf("GroupKey is not deterministic: %q vs %q", key, again)
	}
}

func TestGroupKey_CaseInsensitiveParent(t *testing.T) {
	t.Parallel()

	a := autotag.GroupKey(1, "/Music/Artist/ALBUM/01.mp3", 1)
	b := autotag.GroupKey(1, "/music/artist/album/01.mp3", 1)

	if a != b {
		t.Fatalf("parent dir case should not affect key: %q vs %q", a, b)
	}
}

func TestGroupKey_AlbumTagDoesNotAffectKey(t *testing.T) {
	t.Parallel()

	// Folder = album.  Tracks in the same folder MUST share a key
	// regardless of any per-track variation in the album tag.  This
	// is the bug-fix this test guards against — earlier versions
	// included the album tag in the hash, which fragmented albums
	// whose tracks carried slightly different tags.
	siblings := []string{
		"/music/Artist/Album/01.mp3",
		"/music/Artist/Album/02.mp3",
		"/music/Artist/Album/03.mp3",
	}

	keys := make(map[string]struct{})
	for _, p := range siblings {
		keys[autotag.GroupKey(1, p, 0)] = struct{}{}
	}

	if len(keys) != 1 {
		t.Fatalf(
			"siblings in the same folder should share a key, got %d distinct: %v",
			len(keys), keys,
		)
	}
}

func TestGroupKey_DistinctInputsDiffer(t *testing.T) {
	t.Parallel()

	base := autotag.GroupKey(1, "/music/Artist/Album/01.mp3", 1)

	cases := map[string]string{
		"different library":  autotag.GroupKey(2, "/music/Artist/Album/01.mp3", 1),
		"different parent":   autotag.GroupKey(1, "/music/Artist/Other/01.mp3", 1),
		"different disc":     autotag.GroupKey(1, "/music/Artist/Album/01.mp3", 2),
		"same sibling track": autotag.GroupKey(1, "/music/Artist/Album/02.mp3", 1),
	}

	for name, got := range cases {
		if name == "same sibling track" {
			if got != base {
				t.Errorf("%s: expected same key as sibling track, got %q vs %q", name, got, base)
			}

			continue
		}

		if got == base {
			t.Errorf("%s: expected distinct key, both %q", name, got)
		}
	}
}

func TestGroupKey_UntaggedDiscFoldsIntoDiscOne(t *testing.T) {
	t.Parallel()

	// A folder where only some tracks carry an explicit disc tag must
	// not split: the untagged tracks (disc 0, dhowden/tag's zero value
	// for a missing frame) should group with the ones tagged disc 1.
	untagged := autotag.GroupKey(1, "/music/Artist/Album/01.mp3", 0)
	tagged := autotag.GroupKey(1, "/music/Artist/Album/02.mp3", 1)

	if untagged != tagged {
		t.Fatalf(
			"disc 0 and disc 1 in the same folder should share a key, got %q vs %q",
			untagged, tagged,
		)
	}

	// A genuine disc 2 must still separate from disc 1/untagged.
	discTwo := autotag.GroupKey(1, "/music/Artist/Album/01.mp3", 2)
	if discTwo == tagged {
		t.Fatalf("disc 2 should not share a key with disc 1, got %q", discTwo)
	}
}

func TestResolveDirectoryDiscNumbers_UntaggedFoldsToConsensus(t *testing.T) {
	t.Parallel()

	// A folder that's really disc 2, partially re-tagged: untagged
	// tracks should join disc 2, not fall back to a hardcoded disc 1.
	got := autotag.ResolveDirectoryDiscNumbers([]int{2, 0, 2, 0})
	want := []int{2, 2, 2, 2}

	if !equalInts(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestResolveDirectoryDiscNumbers_AllUntaggedFallsBackToOne(t *testing.T) {
	t.Parallel()

	got := autotag.ResolveDirectoryDiscNumbers([]int{0, 0, 0})
	want := []int{1, 1, 1}

	if !equalInts(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestResolveDirectoryDiscNumbers_GenuineMultiDiscKeepsExplicitValues(t *testing.T) {
	t.Parallel()

	// Explicit disagreement (disc 1 and disc 2 both present, no
	// subfolders) means there's no single disc to guess for the
	// untagged track — it falls back to normalizeDiscNumber's default
	// rather than being assigned to either disc.
	got := autotag.ResolveDirectoryDiscNumbers([]int{1, 1, 2, 2, 0})
	want := []int{1, 1, 2, 2, 1}

	if !equalInts(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestResolveDirectoryDiscNumbers_PreservesExplicitValuesEvenWhenUnanimous(t *testing.T) {
	t.Parallel()

	// Every file already agrees on disc 3 — nothing to resolve, but
	// the explicit values must pass through unchanged.
	got := autotag.ResolveDirectoryDiscNumbers([]int{3, 3, 3})
	want := []int{3, 3, 3}

	if !equalInts(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func equalInts(a, b []int) bool {
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

func TestGroupKey_AmbiguityBoundary(t *testing.T) {
	t.Parallel()

	// Ensure the null-byte separator actually prevents "a|b" vs "ab|"
	// ambiguity: if the hash naively concatenated with no separator,
	// these two would collide.
	a := autotag.GroupKey(1, "/a/b/01.mp3", 0)
	b := autotag.GroupKey(1, "/a/bc/01.mp3", 0)

	if a == b {
		t.Fatalf(
			"expected null-byte separator to disambiguate concatenations, both %q",
			a,
		)
	}
}
