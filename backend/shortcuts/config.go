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

		// Panel-specific (track list)
		"tracklist.play":   "Enter",
		"tracklist.delete": "Delete",
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
