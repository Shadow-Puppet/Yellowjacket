// Package config manages application configuration persistence.
package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path"

	"github.com/BurntSushi/toml"

	"yellowjacket/backend/library"
	"yellowjacket/backend/system"
)

// Config represents the application configuration.
type Config struct {
	ctx      context.Context
	logger   *slog.Logger
	serveMux *http.ServeMux
	filePath string          // required
	Library  *library.Config `form:"Library" schema:"library,required"`

	Window *WindowConfig `toml:"Window"`
}

// NewConfig creates a new config by loading it from disk.
func NewConfig(logger *slog.Logger) (*Config, error) {
	confDir, err := system.GetUserConfigDirPath()
	if err != nil {
		return nil, fmt.Errorf("could not get user config directory: %w", err)
	}

	conf := &Config{
		filePath: path.Join(confDir, "config.toml"),
		serveMux: http.NewServeMux(),
	}
	conf.applyDefaults()
	conf.logger = logger.WithGroup("config").With("config", conf)
	conf.serveMux.HandleFunc("/", conf.handle)

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

	if configErrs != nil {
		return fmt.Errorf("one or more config parts are invalid: %w", configErrs)
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
}

// SetContext sets the Wails runtime context for event emission.
func (c *Config) SetContext(ctx context.Context) {
	c.ctx = ctx
}
