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
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"yellowjacket/backend/events"
	"yellowjacket/backend/library"
	"yellowjacket/backend/system"
	"yellowjacket/backend/theme"
)

// Config represents the application configuration.
type Config struct {
	ctx      context.Context
	logger   *slog.Logger
	filePath string          // required
	Library  *library.Config `toml:"Library"`
	Theme    *theme.Config   `toml:"Theme"`
	Window   *WindowConfig   `toml:"Window"`
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

	return nil
}

// Save writes the config to disk.
func (c *Config) Save() error {
	if err := c.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	confFileData, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("could not marshal config struct: %w", err)
	}

	err = os.WriteFile(c.filePath, confFileData, os.FileMode(int(0o666)))
	if err != nil {
		return fmt.Errorf("could not write config file (%s): %w", c.filePath, err)
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
}

// SetContext sets the Wails runtime context for event emission.
func (c *Config) SetContext(ctx context.Context) {
	c.ctx = ctx
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

	if c.ctx != nil {
		runtime.EventsEmit(
			c.ctx,
			events.LibraryConfigChanged,
			map[string]any{
				"DirectoryPath": dir,
			},
		)
	}

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
	if c.ctx == nil || c.Theme == nil {
		return
	}

	runtime.EventsEmit(
		c.ctx,
		events.ThemeConfigChanged,
		map[string]any{
			"AccentColor":     c.Theme.AccentColor,
			"BackgroundShade": string(c.Theme.BackgroundShade),
		},
	)
}
