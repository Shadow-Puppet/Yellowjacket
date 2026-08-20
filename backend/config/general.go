package config

import (
	"errors"
	"fmt"
)

// DefaultDefaultPage is the launch page for a fresh install.
const DefaultDefaultPage = ViewHome

var (
	errUnknownDefaultPage = errors.New("unknown default page")
	errViewCannotLaunch   = errors.New("view cannot be the launch page")
)

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
	DefaultPage   View          `toml:"DefaultPage"`
	QueueFallback QueueFallback `toml:"QueueFallback"`
	// ViewVisibility says which sidebar destinations are shown, keyed by
	// view id.
	//
	// **An absent key means that view's own default** (`Views`), and that
	// is the whole reason this is a map rather than a `HiddenViews
	// []string` or a struct of booleans. A list's zero value is "hide
	// nothing", which cannot express Autotag being off by default without
	// a migration; a struct field for a view that later stops existing is
	// stored garbage somebody has to deprecate. Here a view added later
	// gets its own default rather than being invisible or forcibly
	// visible, an unknown key is dropped on load, and no install needs
	// migrating in either direction. Same polarity rule as
	// AllowMeteredCatalogDownload: the zero value is the intended answer.
	ViewVisibility map[string]bool `toml:"ViewVisibility"`
	// AllowMeteredCatalogDownload permits the ~0.6 GB Explore catalog to
	// be fetched on a connection the platform calls cellular. It defaults
	// to false, which is the whole point: the zero value is the safe one,
	// so an existing config with no such key refuses by default rather
	// than needing a migration to become careful.
	AllowMeteredCatalogDownload bool `toml:"AllowMeteredCatalogDownload"`
	// PopupVolume draws the bottom bar's volume as a click-to-open popup
	// instead of a slider that is always there (#42).
	//
	// The polarity is the rule this file already states twice: **the
	// zero value is the intended answer**. Inline is the new default, so
	// the flag has to name the *other* choice — an `InlineVolume bool`
	// would default to false and give every existing install the popup
	// this issue exists to stop being the only option, and would need a
	// migration to say otherwise.
	PopupVolume bool `toml:"PopupVolume"`
}

// ApplyDefaults fills zero-value fields with sensible defaults.
//
// A launch page naming a *retired* view is treated as a zero value
// rather than as an error, because the alternative is an app that will
// not start for anyone who had that page selected when it was removed.
// An unknown-but-not-retired name still fails Validate: that is a typo,
// and telling someone about it is the useful answer.
func (c *GeneralConfig) ApplyDefaults() {
	if _, retired := RetiredViews[c.DefaultPage]; retired {
		c.DefaultPage = ""
	}

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

	spec, known := LookupView(string(c.DefaultPage))
	if !known {
		return fmt.Errorf("%w: %q", errUnknownDefaultPage, c.DefaultPage)
	}

	if !spec.CanLaunch {
		return fmt.Errorf("%w: %q", errViewCannotLaunch, c.DefaultPage)
	}

	c.normalizeViewVisibility()

	switch c.QueueFallback {
	case QueueFallbackStop, QueueFallbackFavorites, QueueFallbackDynamicMix:
		// Valid.
	default:
		return fmt.Errorf("%w: %q", errUnknownQueueFallback, c.QueueFallback)
	}

	return nil
}

// normalizeViewVisibility drops what the stored map may not say, and
// repairs the one invariant the shell depends on.
//
// Three things are dropped or forced, and all three are reachable only
// from a hand-edited config or from a version that knew different
// views: an unknown id (a view removed since, e.g. when #27 folds Jobs
// into Settings) says nothing to anybody; a view that is not Hideable
// cannot be false; and **the launch page is always visible**, because
// otherwise an install lands on a page with no nav item pointing at it.
//
// That last one is a *repair* here and an *error* at the setter
// (SetViewVisible), deliberately. On load there is nobody to tell and
// the honest reading of "my launch page is Autotag" is that this user
// wants Autotag, so it is un-hidden rather than the launch page being
// silently reset to something they did not choose. At the setter the
// user is right there and can act, so it refuses and says why.
func (c *GeneralConfig) normalizeViewVisibility() {
	for id := range c.ViewVisibility {
		spec, known := LookupView(id)
		if !known || !spec.Hideable {
			delete(c.ViewVisibility, id)
		}
	}

	if visible, ok := c.ViewVisibility[string(c.DefaultPage)]; ok && !visible {
		c.ViewVisibility[string(c.DefaultPage)] = true
	}
}

// ResolvedViewVisibility answers for every known view, so no caller has
// to know the defaults -- the frontend included, which is why the
// binding returns this rather than the stored map.
func (c *GeneralConfig) ResolvedViewVisibility() map[string]bool {
	resolved := make(map[string]bool, len(Views))

	for _, v := range Views {
		visible := v.VisibleByDefault

		if stored, ok := c.ViewVisibility[string(v.ID)]; ok && v.Hideable {
			visible = stored
		}

		resolved[string(v.ID)] = visible
	}

	return resolved
}
