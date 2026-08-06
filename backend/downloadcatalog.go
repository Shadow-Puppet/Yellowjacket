package backend

import (
	"context"

	"yellowjacket/backend/download"
	"yellowjacket/backend/explore"
)

// exploreCatalog adapts the explore service to the narrow view of the
// music world the wanted list needs.
//
// The adapter lives here, in the composition root, rather than in
// either package: explore should not know that downloads exist, and
// download should not pull in the whole explore index to ask four
// questions.  Everything below is a translation, with no policy of its
// own — policy belongs to the reconciler.
type exploreCatalog struct {
	explore *explore.Service
}

// newExploreCatalog wires the wanted list to the explore index.
func newExploreCatalog(e *explore.Service) download.CatalogPort {
	return &exploreCatalog{explore: e}
}

// ReleaseGroupsForArtist returns an artist's discography.
//
// An empty result is not an error.  The explore index fetches
// discographies lazily, so the first time an artist is subscribed to
// the honest answer is "not indexed yet" — and BrowseReleaseGroups has
// already kicked off the background fetch that makes the next pass
// useful.
func (c *exploreCatalog) ReleaseGroupsForArtist(
	_ context.Context,
	artistMBID string,
) ([]download.CatalogItem, error) {
	groups, err := c.explore.BrowseReleaseGroups(artistMBID)
	if err != nil {
		return nil, err
	}

	out := make([]download.CatalogItem, 0, len(groups))

	for _, rg := range groups {
		out = append(out, download.CatalogItem{
			MBID:             rg.MBID,
			Title:            rg.Title,
			Artist:           rg.ArtistCredit,
			ArtistMBID:       rg.ArtistMBID,
			PrimaryType:      rg.PrimaryType,
			SecondaryTypes:   rg.SecondaryTypes,
			FirstReleaseDate: rg.FirstReleaseDate,
			InLibrary:        rg.InLibrary,
		})
	}

	return out, nil
}

// Tracklist resolves what a want should contain.
//
// For an album this is the tracklist of its best release, which is what
// the download pipeline verifies an unattended grab against.  For a
// single track it is that one track, built from the want's own cached
// title — there is no recording lookup on the index, and inventing one
// to learn a title the UI already passed in would be work for its own
// sake.
func (c *exploreCatalog) Tracklist(
	_ context.Context,
	entity download.Entity,
	mbid string,
) ([]download.ExpectedTrack, error) {
	if entity == download.EntityRecording {
		// Handled by the reconciler from the want's own fields; see
		// recordingTracklist below.
		return nil, nil
	}

	releases, err := c.explore.BrowseReleases(mbid)
	if err != nil {
		return nil, err
	}

	best := bestRelease(releases, mbid)
	if best == nil {
		return nil, nil
	}

	out := make([]download.ExpectedTrack, 0, len(best.Tracks))

	for _, t := range best.Tracks {
		out = append(out, download.ExpectedTrack{
			Position:     t.Position,
			DiscNumber:   t.DiscNumber,
			Title:        t.Title,
			Artist:       best.ArtistCredit,
			LengthMillis: int64(t.Length),
		})
	}

	return out, nil
}

// bestRelease picks which edition of an album to match a download
// against.
//
// When the want named a specific release, that one.  Otherwise the one
// with the most tracks that still has a tracklist at all: a download
// scored against a truncated tracklist looks incomplete when it is
// fine, and a false "missing tracks" reading is what stops a good
// candidate from clearing the auto-pick bar.
func bestRelease(releases []explore.MBRelease, wantedMBID string) *explore.MBRelease {
	var best *explore.MBRelease

	for i := range releases {
		r := &releases[i]

		if r.MBID == wantedMBID && len(r.Tracks) > 0 {
			return r
		}

		if len(r.Tracks) == 0 {
			continue
		}

		if best == nil || len(r.Tracks) > len(best.Tracks) {
			best = r
		}
	}

	return best
}

// Owns reports whether the library already has what an MBID names.
//
// Releases are the gap: the library records release groups and
// recordings, not specific editions, so a want for one particular
// pressing is only retired when its own download completes.  That is
// the right failure — quietly satisfying a "want the 1997 Japanese
// pressing" because some edition is owned would be answering a
// different question than the one asked.
func (c *exploreCatalog) Owns(
	_ context.Context,
	entity download.Entity,
	mbid string,
) (bool, error) {
	if mbid == "" || entity == download.EntityRelease {
		return false, nil
	}

	found := c.explore.CheckLibraryMBIDs([]string{mbid})

	kind, ok := found[mbid]
	if !ok {
		return false, nil
	}

	switch entity {
	case download.EntityReleaseGroup:
		return kind == "release_group", nil
	case download.EntityRecording:
		return kind == "recording", nil
	case download.EntityArtist:
		return kind == "artist", nil
	case download.EntityRelease:
		return false, nil
	default:
		return false, nil
	}
}

// Describe fills in display text for a want added as a bare MBID.
func (c *exploreCatalog) Describe(
	_ context.Context,
	entity download.Entity,
	mbid string,
) (download.CatalogItem, bool) {
	switch entity {
	case download.EntityArtist:
		artist, err := c.explore.LookupArtist(mbid)
		if err != nil || artist == nil {
			return download.CatalogItem{}, false
		}

		return download.CatalogItem{
			MBID:   mbid,
			Artist: artist.Name,
			Title:  artist.Name,
		}, true

	case download.EntityReleaseGroup, download.EntityRelease:
		rg, err := c.explore.LookupReleaseGroup(mbid)
		if err != nil || rg == nil {
			return download.CatalogItem{}, false
		}

		return download.CatalogItem{
			MBID:             mbid,
			Title:            rg.Title,
			Artist:           rg.ArtistCredit,
			ArtistMBID:       rg.ArtistMBID,
			PrimaryType:      rg.PrimaryType,
			SecondaryTypes:   rg.SecondaryTypes,
			FirstReleaseDate: rg.FirstReleaseDate,
			InLibrary:        rg.InLibrary,
		}, true

	case download.EntityRecording:
		// No recording lookup on the index; the caller keeps whatever
		// title it was given.
		return download.CatalogItem{}, false

	default:
		return download.CatalogItem{}, false
	}
}
