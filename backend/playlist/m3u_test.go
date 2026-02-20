package playlist

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple name",
			input:    "My Playlist",
			expected: "my-playlist",
		},
		{
			name:     "special characters",
			input:    "Rock & Roll: Best Of!",
			expected: "rock-roll-best-of",
		},
		{
			name:     "unicode characters",
			input:    "Música Favorita",
			expected: "música-favorita",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "playlist",
		},
		{
			name:     "only special characters",
			input:    "!!!@@@###",
			expected: "playlist",
		},
		{
			name:     "underscores become hyphens",
			input:    "my_cool_playlist",
			expected: "my-cool-playlist",
		},
		{
			name:     "multiple spaces collapse",
			input:    "my   big   playlist",
			expected: "my-big-playlist",
		},
		{
			name:     "leading and trailing hyphens trimmed",
			input:    "  --hello--  ",
			expected: "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := sanitizeFilename(tt.input)
			if result != tt.expected {
				t.Errorf(
					"sanitizeFilename(%q) = %q, want %q",
					tt.input, result, tt.expected,
				)
			}
		})
	}
}

func TestPlaylistFilePath(t *testing.T) {
	t.Parallel()

	result := playlistFilePath("/data/playlists", 42, "My Favorites")
	expected := filepath.Join(
		"/data/playlists", "42-my-favorites.m3u8",
	)

	if result != expected {
		t.Errorf(
			"playlistFilePath() = %q, want %q",
			result, expected,
		)
	}
}

func TestWriteAndParseM3U8(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	entries := []m3uEntry{
		{
			RelativePath: "Artist/Album/01 - Song.flac",
			DurationSec:  243,
			DisplayTitle: "Artist - Song",
		},
		{
			RelativePath: "Other/Track.mp3",
			DurationSec:  180,
			DisplayTitle: "Other - Track",
		},
	}

	err := writeM3U8(dir, 1, "Test Playlist", entries)
	if err != nil {
		t.Fatalf("writeM3U8() error = %v", err)
	}

	// Verify file exists.
	expectedPath := filepath.Join(dir, "1-test-playlist.m3u8")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("expected file %q to exist: %v", expectedPath, err)
	}

	// Parse it back.
	parsed, err := parseM3U8(expectedPath)
	if err != nil {
		t.Fatalf("parseM3U8() error = %v", err)
	}

	if parsed.Name != "Test Playlist" {
		t.Errorf(
			"parsed.Name = %q, want %q",
			parsed.Name, "Test Playlist",
		)
	}

	if len(parsed.Entries) != len(entries) {
		t.Fatalf(
			"parsed %d entries, want %d",
			len(parsed.Entries), len(entries),
		)
	}

	for i, entry := range parsed.Entries {
		if entry.RelativePath != entries[i].RelativePath {
			t.Errorf(
				"entry[%d].RelativePath = %q, want %q",
				i, entry.RelativePath,
				entries[i].RelativePath,
			)
		}

		if entry.DurationSec != entries[i].DurationSec {
			t.Errorf(
				"entry[%d].DurationSec = %d, want %d",
				i, entry.DurationSec,
				entries[i].DurationSec,
			)
		}

		if entry.DisplayTitle != entries[i].DisplayTitle {
			t.Errorf(
				"entry[%d].DisplayTitle = %q, want %q",
				i, entry.DisplayTitle,
				entries[i].DisplayTitle,
			)
		}
	}
}

func TestWriteM3U8EmptyPlaylist(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	err := writeM3U8(dir, 5, "Empty", nil)
	if err != nil {
		t.Fatalf("writeM3U8() error = %v", err)
	}

	parsed, err := parseM3U8(
		filepath.Join(dir, "5-empty.m3u8"),
	)
	if err != nil {
		t.Fatalf("parseM3U8() error = %v", err)
	}

	if parsed.Name != "Empty" {
		t.Errorf("parsed.Name = %q, want %q", parsed.Name, "Empty")
	}

	if len(parsed.Entries) != 0 {
		t.Errorf(
			"parsed %d entries, want 0",
			len(parsed.Entries),
		)
	}
}

func TestWriteM3U8EmptyDir(t *testing.T) {
	t.Parallel()

	err := writeM3U8("", 1, "test", nil)
	if err == nil {
		t.Fatal("expected error for empty dir path")
	}
}

func TestParseM3U8InvalidFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	badFile := filepath.Join(dir, "bad.m3u8")

	// Write a file without the M3U header.
	err := os.WriteFile(
		badFile,
		[]byte("just some text\n"),
		0o644,
	)
	if err != nil {
		t.Fatalf("could not write test file: %v", err)
	}

	_, err = parseM3U8(badFile)
	if err == nil {
		t.Fatal("expected error for invalid M3U file")
	}
}

func TestParseM3U8NonExistentFile(t *testing.T) {
	t.Parallel()

	_, err := parseM3U8("/nonexistent/file.m3u8")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestToRelativePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		absPath     string
		libraryRoot string
		expected    string
	}{
		{
			name:        "normal relative",
			absPath:     "/music/Artist/Album/song.flac",
			libraryRoot: "/music",
			expected:    "Artist/Album/song.flac",
		},
		{
			name:        "path outside library root",
			absPath:     "/other/song.flac",
			libraryRoot: "/music",
			expected:    "/other/song.flac",
		},
		{
			name:        "empty library root",
			absPath:     "/music/song.flac",
			libraryRoot: "",
			expected:    "/music/song.flac",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := toRelativePath(
				tt.absPath, tt.libraryRoot,
			)
			if result != tt.expected {
				t.Errorf(
					"toRelativePath(%q, %q) = %q, want %q",
					tt.absPath, tt.libraryRoot,
					result, tt.expected,
				)
			}
		})
	}
}

func TestToAbsolutePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		relPath     string
		libraryRoot string
		expected    string
	}{
		{
			name:        "relative path",
			relPath:     "Artist/Album/song.flac",
			libraryRoot: "/music",
			expected:    "/music/Artist/Album/song.flac",
		},
		{
			name:        "already absolute",
			relPath:     "/music/song.flac",
			libraryRoot: "/other",
			expected:    "/music/song.flac",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := toAbsolutePath(
				tt.relPath, tt.libraryRoot,
			)
			if result != tt.expected {
				t.Errorf(
					"toAbsolutePath(%q, %q) = %q, want %q",
					tt.relPath, tt.libraryRoot,
					result, tt.expected,
				)
			}
		})
	}
}

func TestDisplayTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		artist   string
		title    string
		expected string
	}{
		{
			name:     "both present",
			artist:   "Artist",
			title:    "Title",
			expected: "Artist - Title",
		},
		{
			name:     "artist only",
			artist:   "Artist",
			title:    "",
			expected: "Artist",
		},
		{
			name:     "title only",
			artist:   "",
			title:    "Title",
			expected: "Title",
		},
		{
			name:     "neither present",
			artist:   "",
			title:    "",
			expected: "Unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := displayTitle(tt.artist, tt.title)
			if result != tt.expected {
				t.Errorf(
					"displayTitle(%q, %q) = %q, want %q",
					tt.artist, tt.title,
					result, tt.expected,
				)
			}
		})
	}
}

func TestExtractPlaylistID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		filePath string
		expected int64
	}{
		{
			name:     "normal ID-prefixed filename",
			filePath: "/data/playlists/42-my-favorites.m3u8",
			expected: 42,
		},
		{
			name:     "no ID prefix",
			filePath: "/data/playlists/my-favorites.m3u8",
			expected: 0,
		},
		{
			name:     "ID only",
			filePath: "/data/playlists/1-.m3u8",
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := extractPlaylistID(tt.filePath)
			if result != tt.expected {
				t.Errorf(
					"extractPlaylistID(%q) = %d, want %d",
					tt.filePath, result, tt.expected,
				)
			}
		})
	}
}

func TestFindPlaylistFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create a playlist file.
	err := writeM3U8(dir, 7, "Test", nil)
	if err != nil {
		t.Fatalf("writeM3U8() error = %v", err)
	}

	// Find it.
	found, err := findPlaylistFile(dir, 7)
	if err != nil {
		t.Fatalf("findPlaylistFile() error = %v", err)
	}

	if found == "" {
		t.Fatal("expected to find playlist file")
	}

	// Try to find a non-existent ID.
	found, err = findPlaylistFile(dir, 999)
	if err != nil {
		t.Fatalf("findPlaylistFile() error = %v", err)
	}

	if found != "" {
		t.Errorf("expected empty string, got %q", found)
	}
}

func TestIsValidM3UExtension(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ext      string
		expected bool
	}{
		{".m3u", true},
		{".m3u8", true},
		{".M3U", true},
		{".M3U8", true},
		{".mp3", false},
		{".txt", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			t.Parallel()

			result := isValidM3UExtension(tt.ext)
			if result != tt.expected {
				t.Errorf(
					"isValidM3UExtension(%q) = %v, want %v",
					tt.ext, result, tt.expected,
				)
			}
		})
	}
}

func TestRemoveOldPlaylistFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create an initial playlist file.
	err := writeM3U8(dir, 3, "Old Name", nil)
	if err != nil {
		t.Fatalf("writeM3U8() error = %v", err)
	}

	oldPath := filepath.Join(dir, "3-old-name.m3u8")
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("old file should exist: %v", err)
	}

	// Write with a new name — should remove the old file.
	err = writeM3U8(dir, 3, "New Name", nil)
	if err != nil {
		t.Fatalf("writeM3U8() error = %v", err)
	}

	// Old file should be gone.
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("old file should have been removed")
	}

	// New file should exist.
	newPath := filepath.Join(dir, "3-new-name.m3u8")
	if _, err := os.Stat(newPath); err != nil {
		t.Errorf("new file should exist: %v", err)
	}
}

func TestListPlaylistFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create some playlist files.
	for i := int64(1); i <= 3; i++ {
		if err := writeM3U8(
			dir, i, "playlist", nil,
		); err != nil {
			t.Fatalf("writeM3U8() error = %v", err)
		}
	}

	// Also create a non-m3u8 file that should be ignored.
	err := os.WriteFile(
		filepath.Join(dir, "notes.txt"),
		[]byte("test"),
		0o644,
	)
	if err != nil {
		t.Fatalf("could not create decoy file: %v", err)
	}

	files, err := listPlaylistFiles(dir)
	if err != nil {
		t.Fatalf("listPlaylistFiles() error = %v", err)
	}

	if len(files) != 3 {
		t.Errorf("found %d files, want 3", len(files))
	}
}

func TestParseExtInf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		line         string
		expectedDur  int
		expectedName string
	}{
		{
			name:         "standard EXTINF",
			line:         "#EXTINF:243,Artist - Title",
			expectedDur:  243,
			expectedName: "Artist - Title",
		},
		{
			name:         "duration only",
			line:         "#EXTINF:180",
			expectedDur:  180,
			expectedName: "",
		},
		{
			name:         "zero duration",
			line:         "#EXTINF:0,Some Title",
			expectedDur:  0,
			expectedName: "Some Title",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dur, title := parseExtInf(tt.line)
			if dur != tt.expectedDur {
				t.Errorf(
					"duration = %d, want %d",
					dur, tt.expectedDur,
				)
			}

			if title != tt.expectedName {
				t.Errorf(
					"title = %q, want %q",
					title, tt.expectedName,
				)
			}
		})
	}
}
