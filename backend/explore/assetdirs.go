package explore

import "path/filepath"

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

// ArtistImageDir returns the directory holding one artist's images.
//
// Artist directories are sharded under the MBID's first two characters,
// which the janitor cannot be expected to know and had got wrong — so
// this is the one definition of that layout, and ArtistImageProvider's
// own artistDir defers to it.
func ArtistImageDir(baseDir, mbid string) string {
	if len(mbid) < 2 {
		return filepath.Join(baseDir, "xx", mbid)
	}

	return filepath.Join(baseDir, mbid[:2], mbid)
}

// ArtistImageKeepNames is every file an artist's directory is meant to
// hold: the portrait, its three size tiers, and the marker recording
// that no portrait was found.  Anything else is a downloaded candidate
// from a version that kept all of them (see StrayArtistImageFilesJob).
func ArtistImageKeepNames() map[string]bool {
	keep := map[string]bool{
		"primary.jpg":  true,
		artistMissFile: true,
	}

	for _, tier := range artistImageTiers {
		keep["primary"+tier.Suffix+".jpg"] = true
	}

	return keep
}
