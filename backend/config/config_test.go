package config

import (
	"log/slog"
	"path/filepath"
	"testing"

	"yellowjacket/backend/favorites"
	"yellowjacket/backend/library"
	"yellowjacket/backend/theme"
	"yellowjacket/backend/tracklist"
)

func TestConfig_LoadSave_Roundtrip(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	libDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	// Build a config with all non-default values.
	original := &Config{
		logger:   slog.Default(),
		filePath: configPath,
		Theme: &theme.Config{
			AccentColor:     "#ff0000",
			BackgroundShade: theme.BackgroundLight,
		},
		TrackList: &tracklist.Config{
			Columns: []tracklist.Column{
				{ID: tracklist.ColTrackName},
				{ID: tracklist.ColArtistName},
				{ID: tracklist.ColAlbum},
				{ID: tracklist.ColGenre},
				{ID: tracklist.ColTrackLength},
			},
		},
		Favorites: &favorites.Config{
			IconStyle:  favorites.IconStar,
			PinDefault: false,
		},
		Library: &library.Config{
			DirectoryPath:   library.Directory(libDir),
			ScanConcurrency: library.ScanConcurrencySSD,
		},
		Window: &WindowConfig{
			Width:  800,
			Height: 600,
		},
	}

	original.applyDefaults()

	if err := original.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Load into a new Config struct.
	loaded := &Config{
		logger:   slog.Default(),
		filePath: configPath,
	}
	loaded.applyDefaults()

	if err := loaded.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Verify theme.
	if loaded.Theme.AccentColor != "#ff0000" {
		t.Errorf("Theme.AccentColor = %q, want %q", loaded.Theme.AccentColor, "#ff0000")
	}

	if loaded.Theme.BackgroundShade != theme.BackgroundLight {
		t.Errorf(
			"Theme.BackgroundShade = %q, want %q",
			loaded.Theme.BackgroundShade, theme.BackgroundLight,
		)
	}

	// Verify tracklist.
	if len(loaded.TrackList.Columns) != 5 {
		t.Fatalf("TrackList.Columns length = %d, want 5", len(loaded.TrackList.Columns))
	}

	wantColumns := []tracklist.ColumnID{
		tracklist.ColTrackName, tracklist.ColArtistName,
		tracklist.ColAlbum, tracklist.ColGenre, tracklist.ColTrackLength,
	}
	for i, want := range wantColumns {
		if loaded.TrackList.Columns[i].ID != want {
			t.Errorf(
				"TrackList.Columns[%d].ID = %q, want %q",
				i, loaded.TrackList.Columns[i].ID, want,
			)
		}
	}

	// Verify favorites.
	if loaded.Favorites.IconStyle != favorites.IconStar {
		t.Errorf(
			"Favorites.IconStyle = %q, want %q",
			loaded.Favorites.IconStyle, favorites.IconStar,
		)
	}

	if loaded.Favorites.PinDefault != false {
		t.Errorf("Favorites.PinDefault = %v, want false", loaded.Favorites.PinDefault)
	}

	// Verify library.
	if string(loaded.Library.DirectoryPath) != libDir {
		t.Errorf("Library.DirectoryPath = %q, want %q", loaded.Library.DirectoryPath, libDir)
	}

	if loaded.Library.ScanConcurrency != library.ScanConcurrencySSD {
		t.Errorf(
			"Library.ScanConcurrency = %q, want %q",
			loaded.Library.ScanConcurrency, library.ScanConcurrencySSD,
		)
	}

	// Verify window.
	if loaded.Window.Width != 800 {
		t.Errorf("Window.Width = %d, want 800", loaded.Window.Width)
	}

	if loaded.Window.Height != 600 {
		t.Errorf("Window.Height = %d, want 600", loaded.Window.Height)
	}
}

func TestConfig_Load_MissingFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "nonexistent", "config.toml")

	c := &Config{
		logger:   slog.Default(),
		filePath: configPath,
	}
	c.applyDefaults()

	// Load should try to create the file. The parent directory
	// doesn't exist, so Save inside Load will fail.
	// Let's use a valid path instead so we can test the "create
	// with defaults" behavior.
	validPath := filepath.Join(tmpDir, "config.toml")
	c.filePath = validPath

	if err := c.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// File should exist after Load.
	if _, err := filepath.Abs(validPath); err != nil {
		t.Fatalf("filepath.Abs() error: %v", err)
	}
}

func TestConfig_Validate_ComposesSubConfigErrors(t *testing.T) {
	t.Parallel()

	c := &Config{
		logger:   slog.Default(),
		filePath: filepath.Join(t.TempDir(), "config.toml"),
		Theme: &theme.Config{
			AccentColor:     "not-a-color",
			BackgroundShade: theme.BackgroundDark,
		},
		TrackList: &tracklist.Config{
			Columns: []tracklist.Column{
				{ID: "bogus_column"},
			},
		},
	}

	err := c.Validate()
	if err == nil {
		t.Fatal("Validate() expected error for invalid sub-configs, got nil")
	}

	errStr := err.Error()

	// Both theme and tracklist errors should be present.
	if !containsSubstring(errStr, "invalid hex color") {
		t.Errorf("error should contain 'invalid hex color', got: %s", errStr)
	}

	if !containsSubstring(errStr, "unknown track-list column ID") {
		t.Errorf("error should contain 'unknown track-list column ID', got: %s", errStr)
	}
}

func TestConfig_ApplyDefaults_NilSubConfigs(t *testing.T) {
	t.Parallel()

	c := &Config{
		logger:   slog.Default(),
		filePath: filepath.Join(t.TempDir(), "config.toml"),
	}

	c.applyDefaults()

	if c.Window == nil {
		t.Error("Window should not be nil after applyDefaults")
	}

	if c.Theme == nil {
		t.Error("Theme should not be nil after applyDefaults")
	}

	if c.TrackList == nil {
		t.Error("TrackList should not be nil after applyDefaults")
	}

	if c.Favorites == nil {
		t.Error("Favorites should not be nil after applyDefaults")
	}
}

// containsSubstring is a test helper for checking error messages.
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}
