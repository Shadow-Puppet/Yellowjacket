// Package config manages application configuration persistence.
package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"

	"github.com/BurntSushi/toml"
	"github.com/wailsapp/wails/v3/pkg/application"

	"yellowjacket/backend/download"
	"yellowjacket/backend/events"
	"yellowjacket/backend/favorites"
	"yellowjacket/backend/library"
	"yellowjacket/backend/shortcuts"
	"yellowjacket/backend/system"
	"yellowjacket/backend/theme"
	"yellowjacket/backend/tracklist"
)

// errSaveBeforeLoad is returned by Save when the in-memory config
// hasn't been hydrated from disk yet.  Prevents writing a default-
// only struct over a real config file during abnormal lifecycle
// sequences (failed startup, racing shutdown).
var errSaveBeforeLoad = errors.New("refusing to save: config not loaded from disk")

// Config represents the application configuration.
type Config struct {
	ctx       context.Context
	logger    *slog.Logger
	filePath  string               // required
	loaded    bool                 // true once Load() succeeds
	Library   *library.Config      `toml:"Library"`
	Theme     *theme.Config        `toml:"Theme"`
	General   *GeneralConfig       `toml:"General"`
	Window    *WindowConfig        `toml:"Window"`
	TrackList *tracklist.Config    `toml:"TrackList"`
	Favorites *favorites.Config    `toml:"Favorites"`
	Shortcuts *shortcuts.Config    `toml:"Shortcuts"`
	Downloads *download.UserConfig `toml:"Downloads"`
}

// NewConfig creates a new config by loading it from disk.
func NewConfig(logger *slog.Logger) (*Config, error) {
	confDir, err := system.GetUserConfigDirPath()
	if err != nil {
		return nil, fmt.Errorf("could not get user config directory: %w", err)
	}

	conf := &Config{
		filePath: path.Join(confDir, "config.toml"),
	}
	conf.applyDefaults()
	conf.logger = logger.WithGroup("config").With("config", conf)

	if err := conf.Load(); err != nil {
		return nil, fmt.Errorf("could not load config: %w", err)
	}

	if err := conf.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return conf, nil
}

// Validate returns errors if there is a breaking issue with the config.
func (c *Config) Validate() error {
	var configErrs error

	if c.Library != nil {
		if len(c.Library.DirectoryPath) != 0 {
			if err := c.Library.Validate(); err != nil {
				configErrs = errors.Join(configErrs, err)
			}
		}
	}

	if c.Theme != nil {
		if err := c.Theme.Validate(); err != nil {
			configErrs = errors.Join(configErrs, err)
		}
	}

	if c.General != nil {
		if err := c.General.Validate(); err != nil {
			configErrs = errors.Join(configErrs, err)
		}
	}

	if c.TrackList != nil {
		if err := c.TrackList.Validate(); err != nil {
			configErrs = errors.Join(configErrs, err)
		}
	}

	if c.Favorites != nil {
		if err := c.Favorites.Validate(); err != nil {
			configErrs = errors.Join(configErrs, err)
		}
	}

	if c.Shortcuts != nil {
		if err := c.Shortcuts.Validate(); err != nil {
			configErrs = errors.Join(configErrs, err)
		}
	}

	if configErrs != nil {
		return fmt.Errorf(
			"one or more config parts are invalid: %w",
			configErrs,
		)
	}

	return nil
}

// Load reads and parses the config file from disk.
func (c *Config) Load() error {
	if _, err := os.Stat(c.filePath); err != nil {
		if os.IsNotExist(err) {
			c.logger.Debug("no config file exists, creating empty config")

			if err := c.Save(); err != nil {
				return fmt.Errorf(
					"could not save empty config to file (%s): %w",
					c.filePath,
					err,
				)
			}
		} else {
			return fmt.Errorf("could not get file info (%s): %w", c.filePath, err)
		}
	}

	// read in the file
	confFileData, err := os.ReadFile(c.filePath)
	if err != nil {
		return fmt.Errorf("problem reading config file %s: %w", c.filePath, err)
	}

	// parse it into the config struct
	_, err = toml.Decode(string(confFileData), c)
	if err != nil {
		return fmt.Errorf("problem parsing config file %s: %w", c.filePath, err)
	}

	c.applyDefaults()

	// validate the config
	if err = c.Validate(); err != nil {
		return fmt.Errorf("invalid config file at %s: %w", c.filePath, err)
	}

	c.logger.Debug("loaded config file", "file", c.filePath)
	c.loaded = true

	return nil
}

// Save writes the config to disk.  Refuses to write if the config
// was never successfully loaded — prevents overwriting user config
// with defaults during abnormal startup/shutdown sequences.
func (c *Config) Save() error {
	if !c.loaded {
		// Allow the initial save when the file doesn't exist yet.
		if _, err := os.Stat(c.filePath); err == nil {
			return errSaveBeforeLoad
		}
	}

	if err := c.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	confFileData, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("could not marshal config struct: %w", err)
	}

	// Write atomically: marshal into a temp file in the same directory,
	// then rename it over the target.  os.WriteFile truncates the file
	// in place before writing, so a crash or kill mid-write (common
	// during dev restarts) can leave a truncated — often empty — config.
	// An empty TOML file loads "successfully" as all-defaults and then
	// gets re-saved as defaults, silently wiping the user's settings.
	// A temp-file + rename makes the replacement atomic: a reader always
	// sees either the previous file or the complete new one.
	tmp, err := os.CreateTemp(path.Dir(c.filePath), "config-*.toml.tmp")
	if err != nil {
		return fmt.Errorf("could not create temp config file: %w", err)
	}

	tmpName := tmp.Name()

	// Best-effort cleanup if we bail before the rename succeeds.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(confFileData); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("could not write temp config file: %w", err)
	}

	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("could not sync temp config file: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("could not close temp config file: %w", err)
	}

	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("could not set config file permissions: %w", err)
	}

	if err := os.Rename(tmpName, c.filePath); err != nil {
		return fmt.Errorf("could not replace config file (%s): %w", c.filePath, err)
	}

	c.logger.Debug("saved config to file", "file", c.filePath)

	return nil
}

// applyDefaults ensures all config sections have valid defaults.
func (c *Config) applyDefaults() {
	if c.Window == nil {
		c.Window = NewDefaultWindowConfig()
	} else {
		c.Window.applyDefaults()
	}

	if c.Library != nil {
		c.Library.ApplyDefaults()
	}

	if c.Theme == nil {
		c.Theme = &theme.Config{}
	}

	c.Theme.ApplyDefaults()

	if c.General == nil {
		c.General = &GeneralConfig{}
	}

	c.General.ApplyDefaults()

	if c.TrackList == nil {
		c.TrackList = &tracklist.Config{}
	}

	c.TrackList.ApplyDefaults()

	if c.Favorites == nil {
		c.Favorites = &favorites.Config{
			PinDefault: true,
		}
	}

	c.Favorites.ApplyDefaults()

	if c.Shortcuts == nil {
		c.Shortcuts = &shortcuts.Config{}
	}

	c.Shortcuts.ApplyDefaults()

	if c.Downloads == nil {
		c.Downloads = &download.UserConfig{}
	}

	c.Downloads.ApplyDefaults()
}

// ServiceStartup is v3's service lifecycle hook: it runs once the
// runtime exists, and ctx is cancelled when the app shuts down.  It
// replaces v2's SetContext, which had to be called by hand from
// OnStartup and was exported, so it was also bound to the frontend.
func (c *Config) ServiceStartup(
	ctx context.Context,
	_ application.ServiceOptions,
) error {
	c.ctx = ctx

	return nil
}

// GetLibraryDirectory returns the currently configured library directory path.
func (c *Config) GetLibraryDirectory() string {
	if c.Library == nil {
		return ""
	}

	return string(c.Library.DirectoryPath)
}

// SetLibraryDirectory validates and saves a new library directory,
// then emits the LibraryConfigChanged event so listeners (e.g. the
// Library scanner) can react.
func (c *Config) SetLibraryDirectory(dir string) error {
	newLibConf, err := library.NewConfig(dir)
	if err != nil {
		return fmt.Errorf(
			"invalid library directory: %w", err,
		)
	}

	// Preserve existing scan concurrency setting.
	if c.Library != nil {
		newLibConf.ScanConcurrency = c.Library.ScanConcurrency
	}

	c.Library = newLibConf

	if err := c.Save(); err != nil {
		return fmt.Errorf(
			"could not save config after directory change: %w", err,
		)
	}

	events.Emit(
		c.ctx,
		events.LibraryConfigChanged,
		map[string]any{
			"DirectoryPath": dir,
		},
	)

	c.logger.Info(
		"library directory updated",
		"directory", dir,
	)

	return nil
}

// GetScanConcurrency returns the configured scan concurrency mode.
func (c *Config) GetScanConcurrency() string {
	if c.Library == nil {
		return string(library.DefaultScanConcurrency)
	}

	return string(c.Library.ScanConcurrency)
}

// SetScanConcurrency validates and saves a new scan concurrency
// mode.  The change takes effect on the next scan.
func (c *Config) SetScanConcurrency(mode string) error {
	if c.Library == nil {
		c.Library = &library.Config{}
		c.Library.ApplyDefaults()
	}

	c.Library.ScanConcurrency = library.ScanConcurrency(
		mode,
	)

	if err := c.Library.Validate(); err != nil {
		return fmt.Errorf(
			"invalid scan concurrency mode: %w", err,
		)
	}

	if err := c.Save(); err != nil {
		return fmt.Errorf(
			"could not save config: %w", err,
		)
	}

	c.logger.Info(
		"scan concurrency updated", "mode", mode,
	)

	return nil
}

// GetDownloadPreferences returns the configured auto-download
// guardrails.
func (c *Config) GetDownloadPreferences() download.AutoDownloadPrefs {
	if c.Downloads == nil {
		return download.AutoDownloadPrefs{}
	}

	return c.Downloads.AutoDownloadPrefs()
}

// SetDownloadPreferences saves new auto-download guardrails.  This only
// persists them; the download package cannot depend on config (config
// already depends on download for UserConfig), so making the change
// live without a restart is the caller's job — the frontend settings
// save calls this and download.Service.SetPreferences in the same
// action, and app.go's initDownloadRuntime applies the saved value to
// the running Manager at startup.
func (c *Config) SetDownloadPreferences(prefs download.AutoDownloadPrefs) error {
	if c.Downloads == nil {
		c.Downloads = &download.UserConfig{}
		c.Downloads.ApplyDefaults()
	}

	formats := make([]string, 0, len(prefs.AllowedFormats))
	for _, f := range prefs.AllowedFormats {
		formats = append(formats, string(f))
	}

	c.Downloads.MinKbps = prefs.MinKbps
	c.Downloads.MaxKbps = prefs.MaxKbps
	c.Downloads.PreferredKbps = prefs.PreferredKbps
	c.Downloads.MaxFileSizeMB = prefs.MaxSizeMB
	c.Downloads.AllowedFormats = formats

	if err := c.Save(); err != nil {
		return fmt.Errorf(
			"could not save config: %w", err,
		)
	}

	c.logger.Info("download auto-pick preferences updated")

	return nil
}

// GetThemeAccentColor returns the configured accent colour.
func (c *Config) GetThemeAccentColor() string {
	if c.Theme == nil {
		return theme.DefaultAccentColor
	}

	return c.Theme.AccentColor
}

// GetThemeBackgroundShade returns the configured background shade.
func (c *Config) GetThemeBackgroundShade() string {
	if c.Theme == nil {
		return string(theme.DefaultBackgroundShade)
	}

	return string(c.Theme.BackgroundShade)
}

// SetThemeAccentColor validates and saves a new accent colour.
func (c *Config) SetThemeAccentColor(
	color string,
) error {
	if c.Theme == nil {
		c.Theme = &theme.Config{}
		c.Theme.ApplyDefaults()
	}

	c.Theme.AccentColor = color

	if err := c.Theme.Validate(); err != nil {
		return fmt.Errorf(
			"invalid theme accent color: %w", err,
		)
	}

	if err := c.Save(); err != nil {
		return fmt.Errorf(
			"could not save config: %w", err,
		)
	}

	c.emitThemeChanged()

	c.logger.Info(
		"theme accent color updated",
		"color", color,
	)

	return nil
}

// SetThemeBackgroundShade validates and saves a new background shade.
func (c *Config) SetThemeBackgroundShade(
	shade string,
) error {
	if c.Theme == nil {
		c.Theme = &theme.Config{}
		c.Theme.ApplyDefaults()
	}

	c.Theme.BackgroundShade = theme.BackgroundShade(shade)

	if err := c.Theme.Validate(); err != nil {
		return fmt.Errorf(
			"invalid theme background shade: %w", err,
		)
	}

	if err := c.Save(); err != nil {
		return fmt.Errorf(
			"could not save config: %w", err,
		)
	}

	c.emitThemeChanged()

	c.logger.Info(
		"theme background shade updated",
		"shade", shade,
	)

	return nil
}

// emitThemeChanged sends the ThemeConfigChanged event to the frontend.
func (c *Config) emitThemeChanged() {
	if c.Theme == nil {
		return
	}

	events.Emit(
		c.ctx,
		events.ThemeConfigChanged,
		map[string]any{
			"AccentColor":     c.Theme.AccentColor,
			"BackgroundShade": string(c.Theme.BackgroundShade),
		},
	)
}

// GetDefaultPage returns the view the app opens to on launch.
func (c *Config) GetDefaultPage() string {
	if c.General == nil {
		return string(DefaultDefaultPage)
	}

	return string(c.General.DefaultPage)
}

// SetDefaultPage validates and saves a new launch page.
func (c *Config) SetDefaultPage(page string) error {
	if c.General == nil {
		c.General = &GeneralConfig{}
		c.General.ApplyDefaults()
	}

	c.General.DefaultPage = View(page)

	if err := c.General.Validate(); err != nil {
		return fmt.Errorf(
			"invalid default page: %w", err,
		)
	}

	if err := c.Save(); err != nil {
		return fmt.Errorf(
			"could not save config: %w", err,
		)
	}

	events.Emit(
		c.ctx,
		events.GeneralConfigChanged,
		map[string]any{
			"DefaultPage": string(c.General.DefaultPage),
		},
	)

	c.logger.Info(
		"default page updated",
		"page", page,
	)

	return nil
}

// GetQueueFallback returns what plays, if anything, once the queue
// runs out.
func (c *Config) GetQueueFallback() string {
	if c.General == nil {
		return string(DefaultQueueFallback)
	}

	return string(c.General.QueueFallback)
}

// SetQueueFallback validates and saves a new queue-fallback mode.
func (c *Config) SetQueueFallback(mode string) error {
	if c.General == nil {
		c.General = &GeneralConfig{}
		c.General.ApplyDefaults()
	}

	c.General.QueueFallback = QueueFallback(mode)

	if err := c.General.Validate(); err != nil {
		return fmt.Errorf(
			"invalid queue fallback: %w", err,
		)
	}

	if err := c.Save(); err != nil {
		return fmt.Errorf(
			"could not save config: %w", err,
		)
	}

	events.Emit(
		c.ctx,
		events.GeneralConfigChanged,
		map[string]any{
			"QueueFallback": string(c.General.QueueFallback),
		},
	)

	c.logger.Info(
		"queue fallback updated",
		"mode", mode,
	)

	return nil
}

// GetAllowMeteredCatalogDownload reports whether the ~0.6 GB Explore
// catalog may be fetched on a metered connection.
func (c *Config) GetAllowMeteredCatalogDownload() bool {
	if c.General == nil {
		return false
	}

	return c.General.AllowMeteredCatalogDownload
}

// SetAllowMeteredCatalogDownload saves the metered-download permission.
//
// There is nothing to validate and nothing to restart: the policy is
// read at the moment a download would start, so turning it on takes
// effect on the next attempt rather than needing this launch to be over.
func (c *Config) SetAllowMeteredCatalogDownload(allow bool) error {
	if c.General == nil {
		c.General = &GeneralConfig{}
		c.General.ApplyDefaults()
	}

	c.General.AllowMeteredCatalogDownload = allow

	if err := c.Save(); err != nil {
		return fmt.Errorf(
			"could not save config: %w", err,
		)
	}

	events.Emit(
		c.ctx,
		events.GeneralConfigChanged,
		map[string]any{
			"AllowMeteredCatalogDownload": allow,
		},
	)

	c.logger.Info(
		"metered catalog download permission updated",
		"allow", allow,
	)

	return nil
}

// GetViewVisibility reports which primary views the sidebar should
// show, answered for every known view rather than only the ones the
// config mentions -- so the frontend filters on a value and never has
// to hold a second copy of the defaults.
func (c *Config) GetViewVisibility() map[string]bool {
	if c.General == nil {
		general := &GeneralConfig{}
		general.ApplyDefaults()

		return general.ResolvedViewVisibility()
	}

	return c.General.ResolvedViewVisibility()
}

// SetViewVisible shows or hides one primary view.
//
// Two refusals, both about a state the user cannot get out of from the
// UI they would be left with: Settings is never hideable, and the
// launch page is never hideable while it is the launch page (change it
// first). Hiding a view does not make it unreachable -- `navigate`
// still resolves it, which detail views depend on -- it only takes the
// nav item away.
func (c *Config) SetViewVisible(view string, visible bool) error {
	spec, known := LookupView(view)
	if !known {
		return fmt.Errorf("%w: %q", errUnknownView, view)
	}

	if c.General == nil {
		c.General = &GeneralConfig{}
		c.General.ApplyDefaults()
	}

	if !visible {
		if !spec.Hideable {
			return fmt.Errorf("%w: %q", errViewNotHideable, view)
		}

		if spec.ID == c.General.DefaultPage {
			return fmt.Errorf("%w: %q", errViewIsLaunchPage, view)
		}
	}

	if c.General.ViewVisibility == nil {
		c.General.ViewVisibility = make(map[string]bool, len(Views))
	}

	c.General.ViewVisibility[view] = visible

	if err := c.General.Validate(); err != nil {
		return fmt.Errorf("invalid view visibility: %w", err)
	}

	if err := c.Save(); err != nil {
		return fmt.Errorf("could not save config: %w", err)
	}

	events.Emit(
		c.ctx,
		events.GeneralConfigChanged,
		map[string]any{
			"ViewVisibility": c.General.ResolvedViewVisibility(),
		},
	)

	c.logger.Info(
		"view visibility updated",
		"view", view,
		"visible", visible,
	)

	return nil
}

// GetTrackListColumns returns the configured track-list columns.
func (c *Config) GetTrackListColumns() []tracklist.Column {
	if c.TrackList == nil {
		return tracklist.DefaultColumns
	}

	return c.TrackList.Columns
}

// SetTrackListColumns validates and saves a new column layout.
func (c *Config) SetTrackListColumns(
	columns []tracklist.Column,
) error {
	if c.TrackList == nil {
		c.TrackList = &tracklist.Config{}
	}

	c.TrackList.Columns = columns

	if err := c.TrackList.Validate(); err != nil {
		return fmt.Errorf(
			"invalid track-list columns: %w", err,
		)
	}

	if err := c.Save(); err != nil {
		return fmt.Errorf(
			"could not save config: %w", err,
		)
	}

	c.emitTrackListChanged()

	c.logger.Info(
		"track-list columns updated",
		"count", len(columns),
	)

	return nil
}

// emitTrackListChanged sends the TrackListConfigChanged event
// to the frontend.
func (c *Config) emitTrackListChanged() {
	if c.ctx == nil || c.TrackList == nil {
		return
	}

	cols := make([]map[string]any, 0, len(c.TrackList.Columns))

	for _, col := range c.TrackList.Columns {
		cols = append(cols, map[string]any{
			"id": string(col.ID),
		})
	}

	events.Emit(
		c.ctx,
		events.TrackListConfigChanged,
		map[string]any{
			"columns": cols,
		},
	)
}

// GetFavoritesPlaylistID returns the configured default playlist ID.
func (c *Config) GetFavoritesPlaylistID() int64 {
	if c.Favorites == nil {
		return 0
	}

	return c.Favorites.PlaylistID
}

// SetFavoritesPlaylistID saves a new default playlist ID.
func (c *Config) SetFavoritesPlaylistID(id int64) error {
	if c.Favorites == nil {
		c.Favorites = &favorites.Config{}
		c.Favorites.ApplyDefaults()
	}

	c.Favorites.PlaylistID = id

	if err := c.Save(); err != nil {
		return fmt.Errorf(
			"could not save config: %w", err,
		)
	}

	c.emitFavoritesChanged()

	c.logger.Info(
		"favorites playlist ID updated",
		"playlistId", id,
	)

	return nil
}

// GetFavoritesIconStyle returns the configured icon style.
func (c *Config) GetFavoritesIconStyle() string {
	if c.Favorites == nil {
		return string(favorites.DefaultIconStyle)
	}

	return string(c.Favorites.IconStyle)
}

// SetFavoritesIconStyle validates and saves a new icon style.
func (c *Config) SetFavoritesIconStyle(
	style string,
) error {
	if c.Favorites == nil {
		c.Favorites = &favorites.Config{}
		c.Favorites.ApplyDefaults()
	}

	c.Favorites.IconStyle = favorites.IconStyle(style)

	if err := c.Favorites.Validate(); err != nil {
		return fmt.Errorf(
			"invalid favorites icon style: %w", err,
		)
	}

	if err := c.Save(); err != nil {
		return fmt.Errorf(
			"could not save config: %w", err,
		)
	}

	c.emitFavoritesChanged()

	c.logger.Info(
		"favorites icon style updated",
		"style", style,
	)

	return nil
}

// GetPinDefaultPlaylist returns whether the default playlist
// is pinned to the top of the playlist view.
func (c *Config) GetPinDefaultPlaylist() bool {
	if c.Favorites == nil {
		return true // default: pinned
	}

	return c.Favorites.PinDefault
}

// SetPinDefaultPlaylist saves whether the default playlist
// should be pinned to the top of the playlist view.
func (c *Config) SetPinDefaultPlaylist(pin bool) error {
	if c.Favorites == nil {
		c.Favorites = &favorites.Config{}
		c.Favorites.ApplyDefaults()
	}

	c.Favorites.PinDefault = pin

	if err := c.Save(); err != nil {
		return fmt.Errorf(
			"could not save config: %w", err,
		)
	}

	c.emitFavoritesChanged()

	c.logger.Info(
		"pin default playlist updated",
		"pin", pin,
	)

	return nil
}

// emitFavoritesChanged sends the FavoritesConfigChanged event
// to the frontend.
func (c *Config) emitFavoritesChanged() {
	if c.Favorites == nil {
		return
	}

	events.Emit(
		c.ctx,
		events.FavoritesConfigChanged,
		map[string]any{
			"PlaylistID": c.Favorites.PlaylistID,
			"IconStyle":  string(c.Favorites.IconStyle),
			"PinDefault": c.Favorites.PinDefault,
		},
	)
}

// GetShortcuts returns the current shortcut bindings map.
func (c *Config) GetShortcuts() map[string]string {
	if c.Shortcuts == nil {
		c.Shortcuts = &shortcuts.Config{}
		c.Shortcuts.ApplyDefaults()
	}

	return c.Shortcuts.Bindings
}

// SetShortcuts saves the entire shortcut bindings map.
func (c *Config) SetShortcuts(
	bindings map[string]string,
) error {
	if c.Shortcuts == nil {
		c.Shortcuts = &shortcuts.Config{}
	}

	c.Shortcuts.Bindings = bindings

	if err := c.Save(); err != nil {
		return fmt.Errorf(
			"could not save shortcuts config: %w", err,
		)
	}

	events.Emit(
		c.ctx,
		events.ShortcutsConfigChanged,
		bindings,
	)

	c.logger.Info("shortcuts config updated")

	return nil
}

// SetShortcut saves a single shortcut binding.
func (c *Config) SetShortcut(
	action string, key string,
) error {
	if c.Shortcuts == nil {
		c.Shortcuts = &shortcuts.Config{}
		c.Shortcuts.ApplyDefaults()
	}

	c.Shortcuts.Bindings[action] = key

	if err := c.Save(); err != nil {
		return fmt.Errorf(
			"could not save shortcut: %w", err,
		)
	}

	events.Emit(
		c.ctx,
		events.ShortcutsConfigChanged,
		c.Shortcuts.Bindings,
	)

	c.logger.Info(
		"shortcut updated",
		"action", action,
		"key", key,
	)

	return nil
}

// ResetShortcuts resets all shortcuts to defaults.
func (c *Config) ResetShortcuts() error {
	c.Shortcuts = &shortcuts.Config{
		Bindings: shortcuts.DefaultBindings(),
	}

	if err := c.Save(); err != nil {
		return fmt.Errorf(
			"could not save shortcuts reset: %w", err,
		)
	}

	events.Emit(
		c.ctx,
		events.ShortcutsConfigChanged,
		c.Shortcuts.Bindings,
	)

	c.logger.Info("shortcuts reset to defaults")

	return nil
}
