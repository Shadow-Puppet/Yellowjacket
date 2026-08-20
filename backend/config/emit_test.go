package config

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"

	"yellowjacket/backend/events"
)

// setupRecordedConfig builds a Config that saves to a temp directory
// and records the events it would push to the frontend.
func setupRecordedConfig(t *testing.T) (*Config, *events.Recorder) {
	t.Helper()

	conf := &Config{
		logger:   slog.Default(),
		filePath: filepath.Join(t.TempDir(), "config.toml"),
	}
	conf.applyDefaults()

	// Load, not just applyDefaults: Save refuses to write a config that
	// was never hydrated from disk, so without this only the first
	// setter in a test succeeds.
	if err := conf.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	rec := events.NewRecorder()
	_ = conf.ServiceStartup(
		events.WithSink(context.Background(), rec),
		application.ServiceOptions{},
	)

	return conf, rec
}

// payloadMap returns the map payload of the most recent named event.
func payloadMap(
	t *testing.T,
	rec *events.Recorder,
	name string,
) map[string]any {
	t.Helper()

	ev, ok := rec.Last(name)
	if !ok {
		t.Fatalf("no %s emitted; got %v", name, rec.Names())
	}

	data, ok := ev.Payload().(map[string]any)
	if !ok {
		t.Fatalf("%s payload is %T, want map[string]any", name, ev.Payload())
	}

	return data
}

// TestEmit_ThemeChangeCarriesBothFields pins that the theme event is a
// snapshot of both fields, not a delta: the frontend applies the whole
// colour ramp from it, so an accent change that omitted the shade would
// re-derive the ramp against a default background.
func TestEmit_ThemeChangeCarriesBothFields(t *testing.T) {
	t.Parallel()

	conf, rec := setupRecordedConfig(t)

	if err := conf.SetThemeBackgroundShade("light"); err != nil {
		t.Fatalf("SetThemeBackgroundShade: %v", err)
	}

	if err := conf.SetThemeAccentColor("#ff0000"); err != nil {
		t.Fatalf("SetThemeAccentColor: %v", err)
	}

	if got := rec.Count(events.ThemeConfigChanged); got != 2 {
		t.Errorf("emitted %d ThemeConfigChanged, want 2", got)
	}

	data := payloadMap(t, rec, events.ThemeConfigChanged)
	if data["AccentColor"] != "#ff0000" {
		t.Errorf("AccentColor = %v, want #ff0000", data["AccentColor"])
	}

	if data["BackgroundShade"] != "light" {
		t.Errorf("BackgroundShade = %v, want light", data["BackgroundShade"])
	}
}

// TestEmit_ThemeChangeIsNotEmittedOnRejectedValue pins that a rejected
// write does not tell the frontend the theme changed.
func TestEmit_ThemeChangeIsNotEmittedOnRejectedValue(t *testing.T) {
	t.Parallel()

	conf, rec := setupRecordedConfig(t)

	if err := conf.SetThemeAccentColor("not-a-colour"); err == nil {
		t.Fatal("SetThemeAccentColor accepted an invalid colour")
	}

	if got := rec.Count(events.ThemeConfigChanged); got != 0 {
		t.Errorf("emitted %d ThemeConfigChanged for a rejected write, want 0", got)
	}
}

// TestEmit_ShortcutChangeSendsWholeBindingMap covers the surface the
// 357-line frontend shortcut service rebuilds itself from.
func TestEmit_ShortcutChangeSendsWholeBindingMap(t *testing.T) {
	t.Parallel()

	conf, rec := setupRecordedConfig(t)

	if err := conf.SetShortcut("playPause", "k"); err != nil {
		t.Fatalf("SetShortcut: %v", err)
	}

	ev, ok := rec.Last(events.ShortcutsConfigChanged)
	if !ok {
		t.Fatalf("no ShortcutsConfigChanged; got %v", rec.Names())
	}

	bindings, ok := ev.Payload().(map[string]string)
	if !ok {
		t.Fatalf("payload is %T, want map[string]string", ev.Payload())
	}

	if bindings["playPause"] != "k" {
		t.Errorf("playPause = %q, want k", bindings["playPause"])
	}

	// The whole map, not just the changed key — the frontend replaces
	// its binding table wholesale on this event.
	if len(bindings) < 2 {
		t.Errorf("emitted %d bindings, want the full default set", len(bindings))
	}
}

func TestEmit_ResetShortcutsRepublishesDefaults(t *testing.T) {
	t.Parallel()

	conf, rec := setupRecordedConfig(t)

	if err := conf.SetShortcut("playPause", "k"); err != nil {
		t.Fatalf("SetShortcut: %v", err)
	}

	rec.Reset()

	if err := conf.ResetShortcuts(); err != nil {
		t.Fatalf("ResetShortcuts: %v", err)
	}

	ev, ok := rec.Last(events.ShortcutsConfigChanged)
	if !ok {
		t.Fatalf("no ShortcutsConfigChanged after reset; got %v", rec.Names())
	}

	bindings, ok := ev.Payload().(map[string]string)
	if !ok {
		t.Fatalf("payload is %T, want map[string]string", ev.Payload())
	}

	if bindings["playPause"] == "k" {
		t.Error("reset emitted the overridden binding, not the default")
	}
}

func TestEmit_FavoritesChangeCarriesFullConfig(t *testing.T) {
	t.Parallel()

	conf, rec := setupRecordedConfig(t)

	if err := conf.SetFavoritesPlaylistID(7); err != nil {
		t.Fatalf("SetFavoritesPlaylistID: %v", err)
	}

	data := payloadMap(t, rec, events.FavoritesConfigChanged)
	if data["PlaylistID"] != int64(7) {
		t.Errorf("PlaylistID = %#v, want int64(7)", data["PlaylistID"])
	}

	for _, key := range []string{"IconStyle", "PinDefault"} {
		if _, ok := data[key]; !ok {
			t.Errorf("payload is missing %q; the settings page reads it", key)
		}
	}
}

// TestEmit_PopupVolumeRoundTripsAndDefaultsToInline pins both halves of
// #42's storage decision.
//
// The **default** is the load-bearing one: inline is what a fresh
// install and an existing `config.toml` with no such key must both
// produce, which is why the field names the popup rather than the
// inline slider. A flag spelled the other way round would default to
// false, hand every existing install the popup this issue exists to
// stop being the only option, and need a migration to say otherwise.
func TestEmit_PopupVolumeRoundTripsAndDefaultsToInline(t *testing.T) {
	t.Parallel()

	conf, rec := setupRecordedConfig(t)

	if conf.GetPopupVolume() {
		t.Error("a config with no PopupVolume key wants the popup, want inline")
	}

	if err := conf.SetPopupVolume(true); err != nil {
		t.Fatalf("SetPopupVolume: %v", err)
	}

	if !conf.GetPopupVolume() {
		t.Error("GetPopupVolume = false after setting it true")
	}

	data := payloadMap(t, rec, events.GeneralConfigChanged)
	if data["PopupVolume"] != true {
		t.Errorf("PopupVolume = %v, want true", data["PopupVolume"])
	}

	if err := conf.SetPopupVolume(false); err != nil {
		t.Fatalf("SetPopupVolume(false): %v", err)
	}

	if conf.GetPopupVolume() {
		t.Error("GetPopupVolume = true after setting it false")
	}
}
