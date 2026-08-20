package config

import "errors"

var (
	errUnknownView      = errors.New("unknown view")
	errViewNotHideable  = errors.New("view cannot be hidden")
	errViewIsLaunchPage = errors.New("view is the launch page")
)

// View identifies one of the shell's primary destinations -- the
// things the sidebar lists and `index.ts` knows as `VIEW_TAGS`.
type View string

// The primary views, in no particular order: the sidebar owns the order
// it draws them in, because that is presentation.
const (
	ViewHome      View = "home"
	ViewPlaylists View = "playlists"
	ViewArtists   View = "artists"
	ViewGenres    View = "genres"
	ViewAlbums    View = "albums"
	ViewTracks    View = "tracks"
	ViewExplore   View = "explore"
	ViewDownloads View = "downloads"
	ViewAutotag   View = "autotag"
	ViewSettings  View = "settings"
)

// RetiredViews are destinations that used to exist and no longer do.
//
// A *visibility* entry for a removed view needs no such list: it is a
// key in a map, and an unknown key is dropped on load. A `DefaultPage`
// is a **value**, and an unknown one fails validation -- which on the
// load path means the app refuses to start rather than a setting being
// ignored. So the one shape that cannot be retired for free is named
// here and reset to the default instead.
//
// `jobs` was folded into Settings by #27: library scans under
// Libraries, index work under Search Index, downloads under the
// download clients, and the autotag apply into the Autotag view.
var RetiredViews = map[View]struct{}{
	"jobs": {},
}

// ViewSpec is what the backend knows about a destination. The label and
// the icon are deliberately absent: those are presentation, they live
// beside the rest of the app's icon vocabulary in
// `frontend/src/utils/icon-language.ts`, and a Go copy of them would be
// a second thing to keep in step for nothing.
type ViewSpec struct {
	// ID is the view name the frontend navigates by.
	ID View
	// VisibleByDefault is what an install gets when the config says
	// nothing about this view -- which is every install until somebody
	// changes it, and every view added after this one shipped.
	VisibleByDefault bool
	// Hideable is false for Settings alone. It is a property of the
	// view rather than a check in the setter because `config.toml` is
	// hand-editable, and an app that can be locked out of its own
	// Settings by a typo is a support problem nobody can debug
	// remotely.
	Hideable bool
	// CanLaunch reports whether the view may be the launch page.
	// Settings is the only one that may not, which is the shape the
	// DefaultPage enum already had.
	CanLaunch bool
}

// Views is the one list of primary destinations, in the order Settings
// offers them.
//
// It is the single source for three things that used to be written down
// separately: which views exist, which of them may be the launch page
// (`DefaultPage`'s validation reads it), and what an unconfigured
// install shows.
//
// Autotag is the one view hidden by default: it rewrites tags on disk,
// which is not what most libraries want on day one, and #25 asks for it
// to be turned on deliberately.
var Views = []ViewSpec{
	{ID: ViewHome, VisibleByDefault: true, Hideable: true, CanLaunch: true},
	{ID: ViewPlaylists, VisibleByDefault: true, Hideable: true, CanLaunch: true},
	{ID: ViewArtists, VisibleByDefault: true, Hideable: true, CanLaunch: true},
	{ID: ViewGenres, VisibleByDefault: true, Hideable: true, CanLaunch: true},
	{ID: ViewAlbums, VisibleByDefault: true, Hideable: true, CanLaunch: true},
	{ID: ViewTracks, VisibleByDefault: true, Hideable: true, CanLaunch: true},
	{ID: ViewExplore, VisibleByDefault: true, Hideable: true, CanLaunch: true},
	{ID: ViewDownloads, VisibleByDefault: true, Hideable: true, CanLaunch: true},
	{ID: ViewAutotag, VisibleByDefault: false, Hideable: true, CanLaunch: true},
	{ID: ViewSettings, VisibleByDefault: true, Hideable: false, CanLaunch: false},
}

// LookupView returns the spec for a view id.
func LookupView(id string) (ViewSpec, bool) {
	for _, v := range Views {
		if string(v.ID) == id {
			return v, true
		}
	}

	return ViewSpec{}, false
}
