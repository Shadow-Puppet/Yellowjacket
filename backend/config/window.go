package config

const (
	// DefaultWidth is the default window width in pixels for a fresh
	// config.  Kept comfortably above the minimum so a first launch
	// (or a config with no saved size) opens at a usable size rather
	// than the cramped minimum.
	DefaultWidth = 1100
	// DefaultHeight is the default window height in pixels for a fresh config.
	DefaultHeight = 720

	// MinWidth is the smallest allowed window width in pixels.  Wails
	// enforces this at runtime; it is also the floor below which a
	// reported size is treated as bogus and not persisted.
	//
	// 800x600 is where the shell was measured to still work, rather
	// than a round number: below ~780 the header's subtitle wraps and
	// pushes the title out of the 4em top bar, and below ~600 tall the
	// eleven sidebar items no longer fit at once.  The previous
	// 512x384 was aspirational — at 700x480 the sidebar overflowed
	// behind the player bar with no scroll and Settings and Jobs could
	// not be reached at all.
	MinWidth = 800
	// MinHeight is the smallest allowed window height in pixels.
	MinHeight = 600
)

// WindowConfig holds window size preferences.
type WindowConfig struct {
	Width  int `toml:"Width"`
	Height int `toml:"Height"`
}

// NewDefaultWindowConfig returns a WindowConfig with sensible defaults.
func NewDefaultWindowConfig() *WindowConfig {
	return &WindowConfig{
		Width:  DefaultWidth,
		Height: DefaultHeight,
	}
}

// applyDefaults fills in zero-value fields with defaults.
func (w *WindowConfig) applyDefaults() {
	if w.Width <= 0 {
		w.Width = DefaultWidth
	}

	if w.Height <= 0 {
		w.Height = DefaultHeight
	}
}
