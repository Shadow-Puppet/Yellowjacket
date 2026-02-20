// Package theme manages visual theme configuration.
package theme

import (
	"errors"
	"fmt"
	"regexp"
)

var (
	errInvalidHexColor        = errors.New("invalid hex color")
	errUnknownBackgroundShade = errors.New("unknown background shade")
)

// hexColorRe matches 3- or 6-digit CSS hex colours (e.g. "#fff", "#ffd43b").
var hexColorRe = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// BackgroundShade controls the base grayscale palette.
type BackgroundShade string

// Valid BackgroundShade values.
const (
	// BackgroundDarker is an OLED-friendly palette with true black.
	BackgroundDarker BackgroundShade = "darker"

	// BackgroundDark is the default dark palette.
	BackgroundDark BackgroundShade = "dark"

	// BackgroundLight is a light-mode palette.
	BackgroundLight BackgroundShade = "light"
)

// Defaults.
const (
	DefaultAccentColor     = "#ffd43b"
	DefaultBackgroundShade = BackgroundDark
)

// Config holds visual theme preferences.
type Config struct {
	AccentColor     string          `toml:"AccentColor"`
	BackgroundShade BackgroundShade `toml:"BackgroundShade"`
}

// ApplyDefaults fills zero-value fields with sensible defaults.
func (c *Config) ApplyDefaults() {
	if c.AccentColor == "" {
		c.AccentColor = DefaultAccentColor
	}

	if c.BackgroundShade == "" {
		c.BackgroundShade = DefaultBackgroundShade
	}
}

// Validate checks that all values are well-formed.
func (c *Config) Validate() error {
	c.ApplyDefaults()

	if !hexColorRe.MatchString(c.AccentColor) {
		return fmt.Errorf(
			"%w: %q",
			errInvalidHexColor,
			c.AccentColor,
		)
	}

	switch c.BackgroundShade {
	case BackgroundDarker, BackgroundDark, BackgroundLight:
		// Valid.
	default:
		return fmt.Errorf(
			"%w: %q",
			errUnknownBackgroundShade,
			c.BackgroundShade,
		)
	}

	return nil
}
