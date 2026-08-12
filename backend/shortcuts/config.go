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

		// Navigation (Global scope)
		"nav.search":    "/",
		"nav.searchAlt": "Ctrl+F",
		"nav.queue":     "Q",

		// App actions (Global scope, Ctrl modifier)
		"app.selectAll": "Ctrl+A",

		// Panel-specific (track list).  There is no `tracklist.delete`:
		// it was bound to Delete and advertised in Settings as
		// configurable while nothing listened for it, because "remove
		// from library" does not exist and it is not clear what it would
		// remove — the row (which the next scan puts back unless the path
		// is also excluded) or the file (a delete-your-music button one
		// keystroke from a focused row).  Advertise it again when it does
		// something.
		"tracklist.play": "Enter",

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
