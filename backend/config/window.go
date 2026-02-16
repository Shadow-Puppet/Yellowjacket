package config

const (
	// DefaultWidth is the default window width in pixels.
	DefaultWidth = 512
	// DefaultHeight is the default window height in pixels.
	DefaultHeight = 384
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
