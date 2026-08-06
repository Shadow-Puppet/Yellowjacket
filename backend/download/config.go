package download

import "time"

// UserConfig is the download subsystem's slice of the TOML config file.
// Provider connections are not here — they live in the database, keyed
// by row, because there can be many of them and they change through the
// settings UI rather than by hand-editing.
type UserConfig struct {
	// PathTemplate lays out imported files under the library root.
	// Tokens: {albumartist} {artist} {album} {year} {track} {disc}
	// {title}.  Empty falls back to DefaultPathTemplate.
	PathTemplate string `toml:"PathTemplate"`

	// AutoPick lets a single high-confidence, high-quality candidate
	// download without asking.  Off by default: an unattended download
	// that picks wrong puts the wrong files in the library, and the
	// ranking has to earn that trust on a given user's sources first.
	AutoPick bool `toml:"AutoPick"`

	// MaxConcurrent bounds simultaneous transfers across all providers.
	// Per-provider limits sit underneath it and are set on the provider
	// itself, since the right number depends on what is on the other
	// end: one Soulseek peer, or a usenet server built for parallelism.
	MaxConcurrent int `toml:"MaxConcurrent"`

	// WantedIntervalMinutes is how often the wanted list is reconciled:
	// artist subscriptions expanded, owned items retired, due wants
	// searched for.  Zero uses the default.
	WantedIntervalMinutes int `toml:"WantedIntervalMinutes"`

	// WantedBatch bounds how many wants one reconcile pass searches
	// for.  A large list should be worked through steadily rather than
	// in one burst that every provider sees as a flood.
	WantedBatch int `toml:"WantedBatch"`
}

// ApplyDefaults fills unset fields.
func (c *UserConfig) ApplyDefaults() {
	if c.PathTemplate == "" {
		c.PathTemplate = DefaultPathTemplate
	}

	if c.MaxConcurrent <= 0 {
		c.MaxConcurrent = defaultConcurrency
	}

	if c.WantedIntervalMinutes <= 0 {
		c.WantedIntervalMinutes = int(defaultReconcileInterval / time.Minute)
	}

	if c.WantedBatch <= 0 {
		c.WantedBatch = defaultDueBatch
	}
}

// WantedInterval is the reconcile interval as a duration.
func (c *UserConfig) WantedInterval() time.Duration {
	return time.Duration(c.WantedIntervalMinutes) * time.Minute
}
