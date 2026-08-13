package config

import (
	"errors"
	"fmt"
)

// DefaultPage identifies which view the app opens to on launch.
type DefaultPage string

// Valid DefaultPage values, matching the frontend's top-level route ids.
const (
	DefaultPageHome      DefaultPage = "home"
	DefaultPageTracks    DefaultPage = "tracks"
	DefaultPageAlbums    DefaultPage = "albums"
	DefaultPageArtists   DefaultPage = "artists"
	DefaultPageGenres    DefaultPage = "genres"
	DefaultPagePlaylists DefaultPage = "playlists"
	DefaultPageExplore   DefaultPage = "explore"
	DefaultPageDownloads DefaultPage = "downloads"
	DefaultPageAutotag   DefaultPage = "autotag"
	DefaultPageJobs      DefaultPage = "jobs"
)

// DefaultDefaultPage is the launch page for a fresh install.
const DefaultDefaultPage = DefaultPageHome

var errUnknownDefaultPage = errors.New("unknown default page")

// QueueFallback identifies what plays, if anything, once the queue
// runs out with nothing left to auto-advance to.
type QueueFallback string

// Valid QueueFallback values.
const (
	QueueFallbackStop       QueueFallback = "stop"
	QueueFallbackFavorites  QueueFallback = "favorites"
	QueueFallbackDynamicMix QueueFallback = "dynamicMix"
)

// DefaultQueueFallback is the fallback behavior for a fresh install.
const DefaultQueueFallback = QueueFallbackFavorites

var errUnknownQueueFallback = errors.New("unknown queue fallback")

// GeneralConfig holds general application preferences that don't
// belong to a more specific subsystem.
type GeneralConfig struct {
	DefaultPage   DefaultPage   `toml:"DefaultPage"`
	QueueFallback QueueFallback `toml:"QueueFallback"`
}

// ApplyDefaults fills zero-value fields with sensible defaults.
func (c *GeneralConfig) ApplyDefaults() {
	if c.DefaultPage == "" {
		c.DefaultPage = DefaultDefaultPage
	}

	if c.QueueFallback == "" {
		c.QueueFallback = DefaultQueueFallback
	}
}

// Validate checks that all values are well-formed.
func (c *GeneralConfig) Validate() error {
	c.ApplyDefaults()

	switch c.DefaultPage {
	case DefaultPageHome, DefaultPageTracks, DefaultPageAlbums, DefaultPageArtists,
		DefaultPageGenres, DefaultPagePlaylists, DefaultPageExplore, DefaultPageDownloads,
		DefaultPageAutotag, DefaultPageJobs:
		// Valid.
	default:
		return fmt.Errorf("%w: %q", errUnknownDefaultPage, c.DefaultPage)
	}

	switch c.QueueFallback {
	case QueueFallbackStop, QueueFallbackFavorites, QueueFallbackDynamicMix:
		// Valid.
	default:
		return fmt.Errorf("%w: %q", errUnknownQueueFallback, c.QueueFallback)
	}

	return nil
}
