// Package library manages the music library and its configuration.
package library

import (
	"errors"
	"fmt"
	"os"
)

var (
	errNotDirectory           = errors.New("path is not a directory")
	errUnknownScanConcurrency = errors.New("unknown scan concurrency mode")
)

// ScanConcurrency controls how many parallel workers the scanner
// uses for metadata extraction.  The choice directly affects I/O
// throughput on spinning disks vs SSDs.
type ScanConcurrency string

// Valid ScanConcurrency modes.
const (
	// ScanConcurrencyAuto detects whether the library resides on
	// a rotational disk and chooses workers accordingly.
	ScanConcurrencyAuto ScanConcurrency = "auto"

	// ScanConcurrencySSD uses runtime.NumCPU() workers, maximising
	// throughput on solid-state storage.
	ScanConcurrencySSD ScanConcurrency = "ssd"

	// ScanConcurrencyHDD uses a small number of workers to limit
	// I/O contention on spinning disks.
	ScanConcurrencyHDD ScanConcurrency = "hdd"
)

// DefaultScanConcurrency is the mode used when no value is
// configured.
const DefaultScanConcurrency = ScanConcurrencyAuto

// Config holds Library config data.
type Config struct {
	DirectoryPath   Directory       `toml:"DirectoryPath"`
	ScanConcurrency ScanConcurrency `toml:"ScanConcurrency"`
}

// Directory represents a filesystem path to a music directory.
type Directory string

// NewConfig creates a validated library configuration.
func NewConfig(dir string) (*Config, error) {
	config := &Config{
		DirectoryPath: Directory(dir),
	}
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf(
			"validation error for new library config: %w",
			err,
		)
	}

	return config, nil
}

// ApplyDefaults fills zero-value fields with sensible defaults.
func (c *Config) ApplyDefaults() {
	if c.ScanConcurrency == "" {
		c.ScanConcurrency = DefaultScanConcurrency
	}
}

// Validate checks that the configured directory exists and that
// the scan concurrency mode is recognised.
func (c *Config) Validate() error {
	c.ApplyDefaults()

	if len(c.DirectoryPath) != 0 {
		dirInfo, err := os.Stat(string(c.DirectoryPath))
		if err != nil {
			return fmt.Errorf(
				"problem getting info on library dir (%s): %w",
				c.DirectoryPath, err,
			)
		}

		if !dirInfo.IsDir() {
			return fmt.Errorf(
				"%s: %w", c.DirectoryPath, errNotDirectory,
			)
		}
	}

	switch c.ScanConcurrency {
	case ScanConcurrencyAuto,
		ScanConcurrencySSD,
		ScanConcurrencyHDD:
		// Valid.
	default:
		return fmt.Errorf(
			"%w: %q", errUnknownScanConcurrency,
			c.ScanConcurrency,
		)
	}

	return nil
}
