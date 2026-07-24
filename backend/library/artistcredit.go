package library

import (
	"strings"

	"yellowjacket/backend/metadata"
)

// featuringSeparators are the credit join phrases that introduce a
// featured (non-primary) artist.  Only true "featuring" markers are
// listed: separators like "&", "x", "with", and "," are deliberately
// excluded because they routinely appear inside real artist names
// (e.g. "Simon & Garfunkel", "Tyler, the Creator").
var featuringSeparators = []string{
	" feat. ", " feat ", " featuring ", " ft. ", " ft ",
}

// stripFeaturing returns the credit up to its first "featuring" marker,
// yielding the primary-artist portion of a credit string.  "Lana Del
// Rey ft. Sean Lennon" becomes "Lana Del Rey"; a credit with no marker
// is returned unchanged (trimmed).  Matching is case-insensitive.
func stripFeaturing(credit string) string {
	lower := strings.ToLower(credit)

	cut := -1

	for _, sep := range featuringSeparators {
		if i := strings.Index(lower, sep); i >= 0 && (cut < 0 || i < cut) {
			cut = i
		}
	}

	if cut < 0 {
		return strings.TrimSpace(credit)
	}

	return strings.TrimSpace(credit[:cut])
}

// primaryArtist resolves the single canonical artist a track credit
// should map to, plus that artist's MusicBrainz ID.  A file tags its
// ARTIST as a full credit string ("Lana Del Rey ft. Sean Lennon") but
// carries only one MUSICBRAINZ_ARTISTID — the primary artist's.  Storing
// the whole credit as an artist entity, and stamping the primary MBID on
// it, is what produced duplicate, mis-titled artists (one MBID fanned
// out across many rows); instead we resolve the primary artist's clean
// name here and keep the full credit only as the artist_credit text.
//
// The clean name comes from the album-artist tag when the track resolves
// to the same MBID as the album artist (the common "Album Artist feat.
// Guest" case, where ALBUMARTIST is the authoritative name).  Otherwise
// the featured clause is stripped from the credit string.
func primaryArtist(tags *metadata.TrackMetadata) (name, mbid string) {
	mbid = tags.ArtistMBID
	if mbid == "" {
		mbid = tags.AlbumArtistMBID
	}

	if tags.ArtistMBID != "" &&
		tags.ArtistMBID == tags.AlbumArtistMBID &&
		tags.AlbumArtist != "" {
		name = tags.AlbumArtist
	} else {
		name = stripFeaturing(tags.Artist)
	}

	if name == "" {
		name = "Unknown Artist"
	}

	return name, mbid
}
