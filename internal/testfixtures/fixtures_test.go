package testfixtures_test

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"path/filepath"
	"testing"

	"yellowjacket/backend/metadata"
	"yellowjacket/internal/testfixtures"
)

// durationToleranceMS is the slack allowed between the nominal length
// in the spec and what a decoder reports.  Lossy encoders pad to a
// frame boundary, so exact equality is not achievable.
const durationToleranceMS = 250

// TestFixturesMatchManifest reads every generated fixture back with the
// application's own metadata extractor and asserts it says what the
// manifest claims.
//
// This is the check that keeps the generator honest: fixtures are
// tagged by backend/tagwriter and read by backend/metadata, so if those
// two ever disagree — a new format, a changed frame ID — it surfaces
// here rather than as a mystery in the UI.
func TestFixturesMatchManifest(t *testing.T) {
	t.Parallel()

	m := testfixtures.Load(t)

	for _, want := range m.Tracks {
		t.Run(want.Path, func(t *testing.T) {
			t.Parallel()

			path := m.Abs(want.Path)

			got, err := metadata.ExtractTags(path)
			if err != nil {
				t.Fatalf("extract tags: %v", err)
			}

			assertTag(t, "title", want, got.Title)
			assertTag(t, "artist", want, got.Artist)
			assertTag(t, "album", want, got.Album)
			assertTag(t, "album_artist", want, got.AlbumArtist)
			assertTag(t, "genre", want, got.Genre)
			assertIntTag(t, "year", want, got.Year)
			assertIntTag(t, "track_number", want, got.TrackNumber)
			assertIntTag(t, "disc_number", want, got.DiscNumber)

			assertCover(t, want, got)
		})
	}
}

// TestFixtureDurationsMatchManifest decodes each fixture and checks its
// length, which is what makes seek, progress and queue-advance
// assertions meaningful elsewhere.
func TestFixtureDurationsMatchManifest(t *testing.T) {
	t.Parallel()

	m := testfixtures.Load(t)

	for _, want := range m.Tracks {
		t.Run(want.Path, func(t *testing.T) {
			t.Parallel()

			got, err := metadata.GetTrackLengthMillis(m.Abs(want.Path))
			if err != nil {
				t.Fatalf("decode duration: %v", err)
			}

			if delta := math.Abs(float64(got - want.DurationMS)); delta > durationToleranceMS {
				t.Errorf(
					"duration: got %dms, want %dms (±%dms)",
					got, want.DurationMS, durationToleranceMS,
				)
			}
		})
	}
}

// TestCoverDedupFixturesShareOneImage guards the premise of the
// cover-dedup case: every track in that album must carry byte-identical
// artwork, or the dedup path is not actually under test.
func TestCoverDedupFixturesShareOneImage(t *testing.T) {
	t.Parallel()

	m := testfixtures.Load(t)

	var first string

	for _, path := range m.Case(t, testfixtures.CaseCoverDedup) {
		tags, err := metadata.ExtractTags(path)
		if err != nil {
			t.Fatalf("extract tags from %s: %v", path, err)
		}

		if tags.Picture == nil {
			t.Fatalf("%s: no embedded cover", filepath.Base(path))
		}

		sum := sha256.Sum256(tags.Picture.Data)
		digest := hex.EncodeToString(sum[:])

		if first == "" {
			first = digest

			continue
		}

		if digest != first {
			t.Errorf(
				"%s: cover differs from the album's first track",
				filepath.Base(path),
			)
		}
	}
}

// TestDuplicateFixturesAreIndistinguishable guards the premise of the
// duplicates case: the pair must agree on everything the duplicate
// detector compares, across two different formats.
func TestDuplicateFixturesAreIndistinguishable(t *testing.T) {
	t.Parallel()

	m := testfixtures.Load(t)

	paths := m.Case(t, testfixtures.CaseDuplicates)
	if len(paths) < 2 {
		t.Fatalf("expected at least two duplicate fixtures, got %d", len(paths))
	}

	ref, err := metadata.ExtractTags(paths[0])
	if err != nil {
		t.Fatalf("extract reference tags: %v", err)
	}

	for _, path := range paths[1:] {
		got, err := metadata.ExtractTags(path)
		if err != nil {
			t.Fatalf("extract tags from %s: %v", path, err)
		}

		if got.Title != ref.Title || got.Artist != ref.Artist ||
			got.Album != ref.Album {
			t.Errorf(
				"%s: (%q, %q, %q) differs from reference (%q, %q, %q)",
				filepath.Base(path),
				got.Title, got.Artist, got.Album,
				ref.Title, ref.Artist, ref.Album,
			)
		}
	}
}

func assertTag(t *testing.T, field string, want testfixtures.Track, got string) {
	t.Helper()

	expected, _ := want.Tags[field].(string)

	if got != expected {
		t.Errorf("%s: got %q, want %q", field, got, expected)
	}
}

func assertIntTag(t *testing.T, field string, want testfixtures.Track, got int) {
	t.Helper()

	// JSON numbers decode as float64.
	expected, _ := want.Tags[field].(float64)

	if got != int(expected) {
		t.Errorf("%s: got %d, want %d", field, got, int(expected))
	}
}

func assertCover(
	t *testing.T,
	want testfixtures.Track,
	got *metadata.TrackMetadata,
) {
	t.Helper()

	if want.CoverSHA == "" {
		if got.Picture != nil {
			t.Errorf("cover: got embedded artwork, want none")
		}

		return
	}

	if got.Picture == nil {
		t.Fatalf("cover: no embedded artwork, want %s", want.CoverSHA[:12])
	}

	sum := sha256.Sum256(got.Picture.Data)

	if digest := hex.EncodeToString(sum[:]); digest != want.CoverSHA {
		t.Errorf("cover: got sha %s, want %s", digest[:12], want.CoverSHA[:12])
	}
}
