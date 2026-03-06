package playlist

import (
	"math"
	"testing"
)

func TestScoreCandidateExactFilename(t *testing.T) {
	t.Parallel()

	pp := newPhantomProfile(
		"/old/path/Artist/Album/01 - Song.flac",
		"Artist - Song",
		243,
	)

	score := scoreCandidate(
		pp,
		"/new/path/Artist/Album/01 - Song.flac",
		"Song",
		"Artist",
		243000,
	)

	if score < 0.9 {
		t.Errorf("expected score >= 0.9, got %f", score)
	}
}

func TestScoreCandidateNoMatch(t *testing.T) {
	t.Parallel()

	pp := newPhantomProfile(
		"/music/Artist/Album/01 - Song.flac",
		"Artist - Song",
		243,
	)

	score := scoreCandidate(
		pp,
		"/music/Completely/Different/track.mp3",
		"Other Title",
		"Other Artist",
		180000,
	)

	if score > 0.3 {
		t.Errorf("expected score <= 0.3, got %f", score)
	}
}

func TestScoreCandidateSameFilenameNewDir(t *testing.T) {
	t.Parallel()

	// Common case: file moved to a different directory.
	pp := newPhantomProfile(
		"/music/Old Dir/Artist/01 - Song.flac",
		"Artist - Song",
		243,
	)

	score := scoreCandidate(
		pp,
		"/music/New Dir/Artist/01 - Song.flac",
		"Song",
		"Artist",
		243000,
	)

	if score < 0.8 {
		t.Errorf(
			"expected score >= 0.8 for same filename, got %f",
			score,
		)
	}
}

func TestScoreCandidateDurationOnly(t *testing.T) {
	t.Parallel()

	// Very close duration, but different filenames.
	score := scoreDuration(243, 243500)
	if score < 0.8 {
		t.Errorf(
			"expected duration score >= 0.8 for ~0.5s diff, got %f",
			score,
		)
	}

	// Exact match.
	score = scoreDuration(180, 180000)
	if score != 1.0 {
		t.Errorf(
			"expected 1.0 for exact match, got %f",
			score,
		)
	}

	// Far apart.
	score = scoreDuration(100, 200000)
	if score != 0.0 {
		t.Errorf(
			"expected 0.0 for 100s diff, got %f",
			score,
		)
	}

	// Unknown duration.
	score = scoreDuration(0, 180000)
	if score != 0.0 {
		t.Errorf(
			"expected 0.0 for unknown, got %f",
			score,
		)
	}
}

func TestScoreFilename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		phantom  string
		cand     string
		minScore float64
		maxScore float64
	}{
		{
			name:     "exact match",
			phantom:  "/a/b/song.flac",
			cand:     "/c/d/song.flac",
			minScore: 1.0,
			maxScore: 1.0,
		},
		{
			name:     "same stem different ext",
			phantom:  "/a/song.flac",
			cand:     "/b/song.mp3",
			minScore: 0.7,
			maxScore: 0.9,
		},
		{
			name:     "completely different",
			phantom:  "/a/song.flac",
			cand:     "/b/other.mp3",
			minScore: 0.0,
			maxScore: 0.2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pp := newPhantomProfile(tt.phantom, "", 0)
			score := scoreFilename(pp, tt.cand)

			if score < tt.minScore || score > tt.maxScore {
				t.Errorf(
					"scoreFilename(%q, %q) = %f, want [%f, %f]",
					tt.phantom, tt.cand,
					score, tt.minScore, tt.maxScore,
				)
			}
		})
	}
}

func TestScoreTitleArtist(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		display  string
		title    string
		artist   string
		minScore float64
	}{
		{
			name:     "exact match",
			display:  "Pink Floyd - Comfortably Numb",
			title:    "Comfortably Numb",
			artist:   "Pink Floyd",
			minScore: 0.9,
		},
		{
			name:     "title only match",
			display:  "Comfortably Numb",
			title:    "Comfortably Numb",
			artist:   "Pink Floyd",
			minScore: 0.7,
		},
		{
			name:     "no match",
			display:  "Something Else",
			title:    "Completely Different",
			artist:   "Other Artist",
			minScore: 0.0,
		},
		{
			name:     "empty display title",
			display:  "",
			title:    "Any Title",
			artist:   "Any Artist",
			minScore: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pp := newPhantomProfile(
				"/dummy/path.flac", tt.display, 0,
			)
			score := scoreTitleArtist(
				pp, tt.title, tt.artist,
			)

			if score < tt.minScore {
				t.Errorf(
					"scoreTitleArtist(%q, %q, %q) = %f, want >= %f",
					tt.display, tt.title, tt.artist,
					score, tt.minScore,
				)
			}
		})
	}
}

func TestExtractKeywords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected []string
	}{
		{
			name: "typical music path",
			path: "/music/Pink Floyd/The Wall/03 - Another Brick in the Wall.flac",
			expected: []string{
				"music", "pink", "floyd", "the",
				"wall", "another", "brick", "in",
			},
		},
		{
			name:     "simple filename",
			path:     "song.mp3",
			expected: []string{"song"},
		},
		{
			name:     "track number stripped",
			path:     "01 - Song Title.flac",
			expected: []string{"song", "title"},
		},
		{
			name:     "empty path",
			path:     "",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := extractKeywords(tt.path)
			if !stringSliceEqual(result, tt.expected) {
				t.Errorf(
					"extractKeywords(%q) = %v, want %v",
					tt.path, result, tt.expected,
				)
			}
		})
	}
}

func TestParseDisplayTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input  string
		artist string
		title  string
	}{
		{
			input:  "Artist - Title",
			artist: "Artist",
			title:  "Title",
		},
		{
			input:  "Just a Title",
			artist: "",
			title:  "Just a Title",
		},
		{
			input:  "",
			artist: "",
			title:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()

			artist, title := parseDisplayTitle(tt.input)
			if artist != tt.artist || title != tt.title {
				t.Errorf(
					"parseDisplayTitle(%q) = (%q, %q), want (%q, %q)",
					tt.input, artist, title,
					tt.artist, tt.title,
				)
			}
		})
	}
}

func TestKeywordOverlap(t *testing.T) {
	t.Parallel()

	// Full overlap.
	score := keywordOverlap(
		[]string{"a", "b", "c"},
		[]string{"a", "b", "c", "d"},
	)

	if score != 1.0 {
		t.Errorf("expected 1.0, got %f", score)
	}

	// Partial overlap.
	score = keywordOverlap(
		[]string{"a", "b", "c"},
		[]string{"a", "d", "e"},
	)

	expected := 1.0 / 3.0
	if math.Abs(score-expected) > 0.01 {
		t.Errorf("expected ~%f, got %f", expected, score)
	}

	// No overlap.
	score = keywordOverlap(
		[]string{"a", "b"},
		[]string{"c", "d"},
	)

	if score != 0.0 {
		t.Errorf("expected 0.0, got %f", score)
	}

	// Empty source.
	score = keywordOverlap(nil, []string{"a"})
	if score != 0.0 {
		t.Errorf("expected 0.0 for empty source, got %f", score)
	}
}

func TestSortCandidatesByScore(t *testing.T) {
	t.Parallel()

	candidates := []CandidateTrack{
		{FilePath: "a", Score: 0.3},
		{FilePath: "b", Score: 0.9},
		{FilePath: "c", Score: 0.6},
	}

	sortCandidatesByScore(candidates)

	if candidates[0].FilePath != "b" {
		t.Errorf(
			"expected first candidate to be 'b', got %q",
			candidates[0].FilePath,
		)
	}

	if candidates[1].FilePath != "c" {
		t.Errorf(
			"expected second candidate to be 'c', got %q",
			candidates[1].FilePath,
		)
	}

	if candidates[2].FilePath != "a" {
		t.Errorf(
			"expected third candidate to be 'a', got %q",
			candidates[2].FilePath,
		)
	}
}

// stringSliceEqual compares two string slices.
func stringSliceEqual(a, b []string) bool {
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
