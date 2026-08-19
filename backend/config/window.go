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
	// **Both reasons this comment used to give have expired**, and the
	// value is right for a third one.  It said the floor was 800x600
	// because "below ~780 the header's subtitle wraps and pushes the
	// title out of the 4em top bar" and "below ~600 tall the eleven
	// sidebar items no longer fit at once".  Neither mechanism can
	// happen now: the subtitle is display:none from 899px down
	// (index.css), and the sidebar host is overflow-y:auto — measured
	// at 600x460, its scrollHeight is 434 against a 332px client and
	// Settings is reachable after scrolling.  A floor defended by two
	// mechanisms that no longer exist is a number nobody can argue
	// with, which is worse than either answer.
	//
	// It stays 800x600 because that is where the *desktop* chrome
	// stops being comfortable — the Compact band of plan 018's size
	// matrix (#24) — and not because the app breaks below it.  It does
	// not: under 600px wide the phone layout takes over (bottom-nav,
	// no sidebar) and the shell fits 320px exactly, which is what
	// makes this a comfort floor rather than a correctness one, and
	// why a very small window reflows instead of becoming a
	// mini-player (#12 is a second always-on-top window, not a mode of
	// this one).
	//
	// The previous 512x384 was aspirational — at 700x480 the sidebar
	// overflowed behind the player bar with no scroll and Settings and
	// Jobs could not be reached at all.
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
