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

	// MinKbps, MaxKbps and PreferredKbps bound and nudge what auto-pick
	// (interactive or via the request list) may grab without asking.
	// Zero on any of them is permissive: see AutoDownloadPrefs.
	//
	// They replaced MinFileSizeMB / MaxFileSizeMB /
	// PreferredFileSizeMB, which were megabytes and so said nothing
	// without knowing how long the release was.  The old keys are
	// deliberately *not* read back: a number that meant "300 MB" cannot
	// be reinterpreted as a bitrate without knowing the album it was
	// aimed at, so migrating it would be inventing an intent the user
	// never expressed.  An existing config falls back to no window,
	// which is the permissive default and matches a fresh install —
	// and MaxFileSizeMB is the one that does carry over, because a
	// ceiling on total bytes still means exactly what it did.
	MinKbps       int `toml:"MinKbps"`
	MaxKbps       int `toml:"MaxKbps"`
	PreferredKbps int `toml:"PreferredKbps"`

	// MaxFileSizeMB is a hard ceiling on a candidate's total size, kept
	// in megabytes on purpose — it is a question about disk space, not
	// about quality, and it has to apply to a candidate whose bitrate
	// cannot be worked out at all.
	MaxFileSizeMB int `toml:"MaxFileSizeMB"`

	// AllowedFormats restricts auto-pick to these formats.  Empty means
	// no restriction.  Values are Format strings ("flac", "mp3", ...).
	AllowedFormats []string `toml:"AllowedFormats"`
}

// AutoDownloadPrefs converts the persisted guardrail fields to the
// runtime type Manager and the ranker consume.
func (c *UserConfig) AutoDownloadPrefs() AutoDownloadPrefs {
	formats := make([]Format, 0, len(c.AllowedFormats))
	for _, f := range c.AllowedFormats {
		formats = append(formats, Format(f))
	}

	return AutoDownloadPrefs{
		MinKbps:        c.MinKbps,
		MaxKbps:        c.MaxKbps,
		PreferredKbps:  c.PreferredKbps,
		MaxSizeMB:      c.MaxFileSizeMB,
		AllowedFormats: formats,
	}
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
