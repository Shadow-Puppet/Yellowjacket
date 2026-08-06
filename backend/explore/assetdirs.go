package explore

// Asset directory names under the user data directory.
//
// These are exported so the maintenance janitor can sweep them without
// importing the internals of the providers that write them.  Each is
// catalogued in backend/datamap as cache data: rebuildable from the
// network, but expensive enough that eviction is by age rather than
// tied to the lifetime of anything else.
const (
	// ArtistImageDirName holds one subdirectory per artist MBID, each
	// containing fetched photos, a primary.jpg with its thumbnails, and
	// possibly a .miss marker recording that no artwork was found.
	ArtistImageDirName = "artist-images"

	// CoverArtCacheDirName holds Cover Art Archive thumbnails fetched
	// while browsing Explore, named by release-group MBID.  Nothing in
	// the database references these files.
	CoverArtCacheDirName = "cover-art-cache"
)
