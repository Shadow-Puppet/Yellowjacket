package config

import (
	"log/slog"
	"path/filepath"
	"testing"

	"yellowjacket/backend/library"
	"yellowjacket/backend/tracklist"
)

// newSavableConfig builds a loaded, valid config in a temp directory,
// so Save() writes rather than refusing with errSaveBeforeLoad.
//
// The library directory is real and set, because Config.Validate only
// validates the Library section when DirectoryPath is non-empty -- an
// empty one would hide a poisoned ScanConcurrency from the whole-config
// save that is the symptom under test.
func newSavableConfig(t *testing.T) *Config {
	t.Helper()

	c := &Config{
		logger:   slog.Default(),
		filePath: filepath.Join(t.TempDir(), "config.toml"),
		Library: &library.Config{
			DirectoryPath: library.Directory(t.TempDir()),
		},
	}

	c.applyDefaults()

	if err := c.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if err := c.Save(); err != nil {
		t.Fatalf("Save() on a fresh config error: %v", err)
	}

	return c
}

// TestSetterRejectionDoesNotPoisonTheConfig is the whole of #231.
//
// Every setter here assigns to the in-memory config and then validates.
// When the validation rejects the argument, the rejected value has to go
// back -- not because the caller sees it (it gets an error either way),
// but because Config.Save() validates the *whole* config.  A value left
// behind by a failed setter therefore fails every later save, of every
// unrelated setting, silently and for the rest of the session.
//
// So each case asserts three things in order: the setter reports the
// error, the getter still reports the old value, and an unrelated save
// still works.  The third is the one the user feels.
func TestSetterRejectionDoesNotPoisonTheConfig(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// reject calls the setter with an argument its own Validate
		// refuses.
		reject func(*Config) error
		// read reports the value the setter writes, so the rollback is
		// asserted on the config rather than only on the save.
		read func(*Config) string
	}{
		{
			name: "scan concurrency",
			reject: func(c *Config) error {
				return c.SetScanConcurrency("telepathy")
			},
			read: (*Config).GetScanConcurrency,
		},
		{
			name: "theme accent colour",
			reject: func(c *Config) error {
				return c.SetThemeAccentColor("not-a-hex")
			},
			read: (*Config).GetThemeAccentColor,
		},
		{
			name: "theme background shade",
			reject: func(c *Config) error {
				return c.SetThemeBackgroundShade("chartreuse")
			},
			read: (*Config).GetThemeBackgroundShade,
		},
		{
			name: "default page",
			reject: func(c *Config) error {
				return c.SetDefaultPage("nowhere")
			},
			read: (*Config).GetDefaultPage,
		},
		{
			name: "queue fallback",
			reject: func(c *Config) error {
				return c.SetQueueFallback("improvise")
			},
			read: (*Config).GetQueueFallback,
		},
		{
			name: "favorites icon style",
			reject: func(c *Config) error {
				return c.SetFavoritesIconStyle("asterisk")
			},
			read: (*Config).GetFavoritesIconStyle,
		},
		{
			name: "track-list columns",
			reject: func(c *Config) error {
				// titleArtist is a drawing definition, not a
				// configurable column (#197), so it is exactly what
				// the frontend used to be able to send.
				return c.SetTrackListColumns([]tracklist.Column{
					{ID: "titleArtist"},
				})
			},
			read: func(c *Config) string {
				return columnIDs(c.GetTrackListColumns())
			},
		},
		{
			name: "track-list columns, duplicated",
			reject: func(c *Config) error {
				// The route #197 closed was one invalid id; a
				// duplicate is the one still reachable from a client
				// that assembles the list itself.
				return c.SetTrackListColumns([]tracklist.Column{
					{ID: tracklist.ColTrackName},
					{ID: tracklist.ColTrackName},
				})
			},
			read: func(c *Config) string {
				return columnIDs(c.GetTrackListColumns())
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := newSavableConfig(t)
			before := tc.read(c)

			if err := tc.reject(c); err == nil {
				t.Fatal("setter accepted an invalid value, want an error")
			}

			if after := tc.read(c); after != before {
				t.Errorf(
					"value after a rejected write = %q, want the previous %q",
					after, before,
				)
			}

			// The symptom: an unrelated setting can no longer be saved.
			if err := c.SetPopupVolume(true); err != nil {
				t.Errorf("an unrelated setter failed after a rejected write: %v", err)
			}

			if err := c.Save(); err != nil {
				t.Errorf("Save() failed after a rejected write: %v", err)
			}
		})
	}
}

// TestRejectedSetterLeavesNothingOnDisk pairs with the sweep above: the
// rollback must not be undone by what the file already holds, so a
// config reloaded from disk after a rejected write agrees with memory.
func TestRejectedSetterLeavesNothingOnDisk(t *testing.T) {
	t.Parallel()

	c := newSavableConfig(t)

	if err := c.SetThemeAccentColor("#123456"); err != nil {
		t.Fatalf("SetThemeAccentColor() error: %v", err)
	}

	if err := c.SetThemeAccentColor("not-a-hex"); err == nil {
		t.Fatal("SetThemeAccentColor accepted a non-colour, want an error")
	}

	reloaded := &Config{logger: slog.Default(), filePath: c.filePath}
	reloaded.applyDefaults()

	if err := reloaded.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if got := reloaded.GetThemeAccentColor(); got != "#123456" {
		t.Errorf("accent colour on disk = %q, want %q", got, "#123456")
	}

	if c.GetThemeAccentColor() != reloaded.GetThemeAccentColor() {
		t.Errorf(
			"in-memory accent %q disagrees with disk %q after a rejected write",
			c.GetThemeAccentColor(), reloaded.GetThemeAccentColor(),
		)
	}
}

// TestSetLibraryDirectoryValidatesBeforeAssigning pins the precedent the
// seven rolled-back setters follow: this one has always built and
// validated a candidate before assigning, so a bad path never reaches
// the config at all.
func TestSetLibraryDirectoryValidatesBeforeAssigning(t *testing.T) {
	t.Parallel()

	c := newSavableConfig(t)
	before := c.GetLibraryDirectory()

	if err := c.SetLibraryDirectory(filepath.Join(t.TempDir(), "no-such-dir")); err == nil {
		t.Fatal("SetLibraryDirectory accepted a missing directory, want an error")
	}

	if after := c.GetLibraryDirectory(); after != before {
		t.Errorf("library directory = %q, want the previous %q", after, before)
	}

	if err := c.Save(); err != nil {
		t.Errorf("Save() failed after a rejected library directory: %v", err)
	}
}

// TestSetViewVisibleRefusesBeforeAssigning covers the other setter left
// out of the rollback pass: it guards its own argument up front, so
// GeneralConfig.Validate never sees a view it would reject.
func TestSetViewVisibleRefusesBeforeAssigning(t *testing.T) {
	t.Parallel()

	c := newSavableConfig(t)

	if err := c.SetViewVisible("no-such-view", false); err == nil {
		t.Fatal("SetViewVisible accepted an unknown view, want an error")
	}

	if err := c.SetViewVisible(c.GetDefaultPage(), false); err == nil {
		t.Fatal("SetViewVisible hid the launch page, want an error")
	}

	if err := c.Save(); err != nil {
		t.Errorf("Save() failed after a refused view visibility change: %v", err)
	}
}

// columnIDs renders a column list for comparison in the table above.
func columnIDs(cols []tracklist.Column) string {
	ids := make([]byte, 0, len(cols)*8)

	for i, col := range cols {
		if i > 0 {
			ids = append(ids, ',')
		}

		ids = append(ids, col.ID...)
	}

	return string(ids)
}
