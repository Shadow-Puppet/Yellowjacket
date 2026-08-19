// Package shortcuts manages keyboard shortcut configuration.
package shortcuts

// Config holds user-customized keyboard shortcut bindings.
// Keys are action IDs (e.g. "player.playPause"), values are
// key combo strings in canonical format (e.g. "Ctrl+F", "Space").
type Config struct {
	Bindings map[string]string `toml:"Bindings"`
}

// DefaultBindings returns the default keyboard shortcut bindings.
// Follows hybrid style: Space/arrows for player, Ctrl+key for app actions.
func DefaultBindings() map[string]string {
	return map[string]string{
		// Player controls (Global scope, no modifier)
		"player.playPause":   "Space",
		"player.next":        "N",
		"player.previous":    "P",
		"player.volumeUp":    "Up",
		"player.volumeDown":  "Down",
		"player.seekForward": "Right",
		"player.seekBack":    "Left",
		"player.shuffle":     "S",
		"player.repeat":      "R",
		"player.mute":        "M",

		// Navigation (Global scope).  Back and forward are the browser's
		// own combination on every platform, which is the whole design
		// brief for them: the app has one global history and this is the
		// gesture people already have for it.  The modifier is what keeps
		// them clear of `player.seekBack`/`seekForward`, which are the
		// bare arrows -- a binding is matched on its full canonical
		// string, so "Alt+Left" and "Left" are different keys and not a
		// conflict.
		"nav.search":    "/",
		"nav.searchAlt": "Ctrl+F",
		"nav.queue":     "Q",
		"nav.back":      "Alt+Left",
		"nav.forward":   "Alt+Right",

		// App actions
		"app.selectAll": "Ctrl+A",
		"app.shortcuts": "?",

		// Panel-specific (track list).  `tracklist.delete` spent six
		// phases advertised in Settings with nothing on the other end of
		// it, because "remove from library" did not exist and it was not
		// clear what it would remove.  It now removes the row and
		// excludes the path from future scans, and leaves the file on
		// disk — and the key only *opens the confirmation*, never
		// performs the removal, which is the only version defensible one
		// keystroke from a focused row.
		"tracklist.play":   "Enter",
		"tracklist.delete": "Delete",

		// Panel-specific (autotag review).  These are the keys the
		// autotag page used to bind on its own document listener, which
		// fired from every other page and could not arbitrate with the
		// global bindings above.  As panel bindings they apply only
		// while that page is the one on screen.
		"autotag.apply":    "A",
		"autotag.skip":     "S",
		"autotag.leave":    "L",
		"autotag.paste":    "U",
		"autotag.search":   "F",
		"autotag.next":     "Down",
		"autotag.previous": "Up",
	}
}

// ApplyDefaults fills any missing bindings with defaults.
// Existing user customizations are preserved.
func (c *Config) ApplyDefaults() {
	if c.Bindings == nil {
		c.Bindings = DefaultBindings()

		return
	}

	defaults := DefaultBindings()
	for action, key := range defaults {
		if _, exists := c.Bindings[action]; !exists {
			c.Bindings[action] = key
		}
	}
}

// Validate checks that the config is well-formed.
func (c *Config) Validate() error {
	c.ApplyDefaults()
	// No validation errors possible — any string is a valid binding.
	// Conflict detection is a frontend UX concern, not a config error.
	return nil
}
