// Package favorites manages the default playlist configuration.
package favorites

import (
	"errors"
	"fmt"
)

var errUnknownIconStyle = errors.New(
	"unknown favorites icon style",
)

// IconStyle controls the icon used to indicate favourited tracks.
type IconStyle string

// Valid IconStyle values.
const (
	// IconHeart uses a heart icon.
	IconHeart IconStyle = "heart"

	// IconStar uses a star icon.
	IconStar IconStyle = "star"
)

// DefaultIconStyle is applied when no value has been set.
const DefaultIconStyle = IconHeart

// DefaultPlaylistName is the name given to the auto-created
// default playlist.
const DefaultPlaylistName = "Favorites"

// Config holds favourites preferences.
type Config struct {
	PlaylistID int64     `toml:"PlaylistID"`
	IconStyle  IconStyle `toml:"IconStyle"`
}

// ApplyDefaults fills zero-value fields with sensible defaults.
func (c *Config) ApplyDefaults() {
	if c.IconStyle == "" {
		c.IconStyle = DefaultIconStyle
	}
}

// Validate checks that all values are well-formed.
func (c *Config) Validate() error {
	c.ApplyDefaults()

	switch c.IconStyle {
	case IconHeart, IconStar:
		// Valid.
	default:
		return fmt.Errorf(
			"%w: %q",
			errUnknownIconStyle,
			c.IconStyle,
		)
	}

	return nil
}
