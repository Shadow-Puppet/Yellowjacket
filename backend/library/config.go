// Package library manages the music library and its configuration.
package library

import (
	"errors"
	"fmt"
	"os"
)

var errNotDirectory = errors.New("path is not a directory")

// Config holds Library config data.
type Config struct {
	DirectoryPath Directory `form:"Directory" schema:"directory,required"`
}

// Directory represents a filesystem path to a music directory.
type Directory string

// NewConfig creates a validated library configuration.
func NewConfig(dir string) (*Config, error) {
	config := &Config{
		DirectoryPath: Directory(dir),
	}
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("validation error for new library config: %w", err)
	}

	return config, nil
}

// Validate checks that the configured directory exists.
func (c *Config) Validate() error {
	if len(c.DirectoryPath) != 0 {
		dirInfo, err := os.Stat(string(c.DirectoryPath))
		if err != nil {
			return fmt.Errorf("problem getting info on library dir (%s): %w", c.DirectoryPath, err)
		}

		if !dirInfo.IsDir() {
			return fmt.Errorf("%s: %w", c.DirectoryPath, errNotDirectory)
		}
	}

	return nil
}
