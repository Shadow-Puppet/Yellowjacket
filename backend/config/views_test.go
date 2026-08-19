package config

import (
	"errors"
	"log/slog"
	"path/filepath"
	"testing"
)

// newViewTestConfig builds a Config backed by a temp file, which is all
// SetViewVisible needs: it saves and emits, and the emit is a no-op
// without a running app.
func newViewTestConfig(t *testing.T) *Config {
	t.Helper()

	c := &Config{
		logger:   slog.Default(),
		filePath: filepath.Join(t.TempDir(), "config.toml"),
	}

	// Load a file that is not there: that is what marks the config
	// loaded, without which Save refuses on the *second* write.
	if err := c.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	return c
}

// A view the config says nothing about takes its own default, which is
// what makes this need no migration in either direction: an existing
// install gets Autotag hidden without a key, and a view added later
// gets its own answer rather than the list's.
func TestViewVisibilityDefaults(t *testing.T) {
	t.Parallel()

	general := &GeneralConfig{}
	general.ApplyDefaults()

	resolved := general.ResolvedViewVisibility()

	if len(resolved) != len(Views) {
		t.Fatalf("resolved %d views, want %d", len(resolved), len(Views))
	}

	if resolved[string(ViewAutotag)] {
		t.Error("autotag should be hidden by default")
	}

	for _, v := range Views {
		if v.ID == ViewAutotag {
			continue
		}

		if !resolved[string(v.ID)] {
			t.Errorf("%s should be visible by default", v.ID)
		}
	}
}

// A stored answer wins over the default, in both directions -- turning
// Autotag on is the whole user-facing point.
func TestViewVisibilityStoredWins(t *testing.T) {
	t.Parallel()

	general := &GeneralConfig{
		ViewVisibility: map[string]bool{
			string(ViewAutotag): true,
			string(ViewJobs):    false,
		},
	}
	general.ApplyDefaults()

	resolved := general.ResolvedViewVisibility()

	if !resolved[string(ViewAutotag)] {
		t.Error("autotag was switched on and should be visible")
	}

	if resolved[string(ViewJobs)] {
		t.Error("jobs was switched off and should be hidden")
	}
}

// A key for a view that no longer exists is discarded rather than
// migrated. This is the property the #25-before-#27 ordering rests on:
// when Jobs folds into Settings, `jobs = true` in somebody's config is
// a key nothing asks about, not a cleanup task.
func TestValidateDropsUnknownAndUnhideableViews(t *testing.T) {
	t.Parallel()

	general := &GeneralConfig{
		ViewVisibility: map[string]bool{
			"a-view-that-was-removed": true,
			string(ViewSettings):      false,
			string(ViewAutotag):       true,
		},
	}

	if err := general.Validate(); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}

	if _, ok := general.ViewVisibility["a-view-that-was-removed"]; ok {
		t.Error("an unknown view id should be dropped on load")
	}

	if _, ok := general.ViewVisibility[string(ViewSettings)]; ok {
		t.Error("settings is not hideable and should not be stored")
	}

	if !general.ResolvedViewVisibility()[string(ViewSettings)] {
		t.Error("settings must resolve visible whatever the file said")
	}
}

// On load there is nobody to tell, so a launch page hidden by a
// hand-edited file is un-hidden rather than the launch page being
// reset to something the user did not choose.
func TestValidateRevealsAHiddenLaunchPage(t *testing.T) {
	t.Parallel()

	general := &GeneralConfig{
		DefaultPage: ViewAutotag,
		ViewVisibility: map[string]bool{
			string(ViewAutotag): false,
		},
	}

	if err := general.Validate(); err != nil {
		t.Fatalf("Validate() error: %v", err)
	}

	if !general.ResolvedViewVisibility()[string(ViewAutotag)] {
		t.Error("the launch page must be visible")
	}
}

// Settings may not be the launch page, which is the shape the old
// DefaultPage enum had and is now read off the same table.
func TestValidateRejectsAnUnlaunchablePage(t *testing.T) {
	t.Parallel()

	general := &GeneralConfig{DefaultPage: ViewSettings}

	err := general.Validate()
	if !errors.Is(err, errViewCannotLaunch) {
		t.Fatalf("Validate() error = %v, want errViewCannotLaunch", err)
	}
}

// At the setter the user is present and can act, so the two states
// they could not get out of are refused rather than repaired.
func TestSetViewVisibleRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		view    string
		visible bool
		want    error
	}{
		{"settings is never hideable", string(ViewSettings), false, errViewNotHideable},
		{"the launch page is not hideable", string(ViewHome), false, errViewIsLaunchPage},
		{"an unknown view is not a setting", "nonsense", false, errUnknownView},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := newViewTestConfig(t)

			err := c.SetViewVisible(tt.view, tt.visible)
			if !errors.Is(err, tt.want) {
				t.Fatalf("SetViewVisible() error = %v, want %v", err, tt.want)
			}
		})
	}
}

// Showing a view is never refused, including Settings and the launch
// page -- there is no state to be stuck in.
func TestSetViewVisibleShowsAnything(t *testing.T) {
	t.Parallel()

	c := newViewTestConfig(t)

	for _, v := range Views {
		if err := c.SetViewVisible(string(v.ID), true); err != nil {
			t.Fatalf("SetViewVisible(%q, true) error: %v", v.ID, err)
		}
	}

	if !c.GetViewVisibility()[string(ViewAutotag)] {
		t.Error("autotag was switched on and should be visible")
	}
}

// The stored map survives a save/load round trip, which is what a
// map-valued TOML key is worth checking for.
func TestViewVisibilityRoundTrips(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.toml")

	original := &Config{logger: slog.Default(), filePath: path}
	if err := original.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if err := original.SetViewVisible(string(ViewAutotag), true); err != nil {
		t.Fatalf("SetViewVisible() error: %v", err)
	}

	if err := original.SetViewVisible(string(ViewJobs), false); err != nil {
		t.Fatalf("SetViewVisible() error: %v", err)
	}

	loaded := &Config{logger: slog.Default(), filePath: path}
	if err := loaded.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	resolved := loaded.GetViewVisibility()

	if !resolved[string(ViewAutotag)] {
		t.Error("autotag should have loaded as visible")
	}

	if resolved[string(ViewJobs)] {
		t.Error("jobs should have loaded as hidden")
	}
}

// Every view the shell can launch into is a view the sidebar can show,
// or an install could land on a page with no nav item and no setting
// pointing at it.
func TestEveryLaunchableViewIsAView(t *testing.T) {
	t.Parallel()

	for _, v := range Views {
		if !v.CanLaunch {
			continue
		}

		if !v.Hideable {
			continue
		}

		if _, ok := LookupView(string(v.ID)); !ok {
			t.Errorf("%s is launchable but not a known view", v.ID)
		}
	}
}
