package explore

import (
	"context"

	"yellowjacket/backend/autotag"
)

// AutotagClient adapts *MusicBrainzClient to autotag.MBClient.
// Lives in the explore package so autotag stays free of
// explore-internal types — callers in app wiring construct one via
// NewAutotagClient and hand it to the scorer.
type AutotagClient struct {
	inner *MusicBrainzClient
}

// NewAutotagClient wraps a MusicBrainzClient for use by the
// autotag scorer.
func NewAutotagClient(inner *MusicBrainzClient) *AutotagClient {
	return &AutotagClient{inner: inner}
}

// SearchReleaseGroups delegates to the wrapped client and projects
// hits into autotag's minimal shape.
func (c *AutotagClient) SearchReleaseGroups(
	ctx context.Context, query string, limit int,
) ([]autotag.MBReleaseGroupHit, int, error) {
	hits, total, err := c.inner.SearchReleaseGroups(ctx, query, limit)
	if err != nil {
		return nil, 0, err
	}

	out := make([]autotag.MBReleaseGroupHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, autotag.MBReleaseGroupHit{
			MBID:         h.MBID,
			Title:        h.Title,
			ArtistCredit: h.ArtistCredit,
			FirstDate:    h.FirstReleaseDate,
			PrimaryType:  h.PrimaryType,
		})
	}

	return out, total, nil
}

// BrowseReleases delegates to the wrapped client and projects each
// release (and its tracks) into autotag's shape.  Length is
// millisecond-aligned to match local audio_files.
func (c *AutotagClient) BrowseReleases(
	ctx context.Context, releaseGroupMBID string,
) ([]autotag.MBRelease, error) {
	releases, err := c.inner.BrowseReleases(ctx, releaseGroupMBID)
	if err != nil {
		return nil, err
	}

	out := make([]autotag.MBRelease, 0, len(releases))
	for _, rel := range releases {
		out = append(out, exploreToAutotagRelease(rel))
	}

	return out, nil
}

// LookupRelease fetches a single release by MBID and projects it
// into autotag's shape.
func (c *AutotagClient) LookupRelease(
	ctx context.Context, releaseMBID string,
) (autotag.MBRelease, error) {
	rel, err := c.inner.LookupRelease(ctx, releaseMBID)
	if err != nil {
		return autotag.MBRelease{}, err
	}

	return exploreToAutotagRelease(*rel), nil
}

// LookupReleaseGroup fetches a single release group by MBID and
// projects it into autotag's hit shape.
func (c *AutotagClient) LookupReleaseGroup(
	ctx context.Context, releaseGroupMBID string,
) (autotag.MBReleaseGroupHit, error) {
	rg, err := c.inner.LookupReleaseGroup(ctx, releaseGroupMBID)
	if err != nil {
		return autotag.MBReleaseGroupHit{}, err
	}

	return autotag.MBReleaseGroupHit{
		MBID:         rg.MBID,
		Title:        rg.Title,
		ArtistCredit: rg.ArtistCredit,
		FirstDate:    rg.FirstReleaseDate,
		PrimaryType:  rg.PrimaryType,
	}, nil
}

// LookupArtist returns the artist's sort name when available,
// falling back to the display name.  Used by the resolver when
// constructing Lucene-style fallback queries.
func (c *AutotagClient) LookupArtist(
	ctx context.Context, mbid string,
) (string, error) {
	a, err := c.inner.LookupArtist(ctx, mbid)
	if err != nil {
		return "", err
	}

	if a.SortName != "" {
		return a.SortName, nil
	}

	return a.Name, nil
}

func exploreToAutotagRelease(rel MBRelease) autotag.MBRelease {
	tracks := make([]autotag.CandidateTrack, 0, len(rel.Tracks))
	for _, t := range rel.Tracks {
		tracks = append(tracks, autotag.CandidateTrack{
			Position:     t.Position,
			DiscNumber:   t.DiscNumber,
			Title:        t.Title,
			LengthMillis: int64(t.Length),
			MBID:         t.MBID,
		})
	}

	return autotag.MBRelease{
		MBID:         rel.MBID,
		Title:        rel.Title,
		Date:         rel.Date,
		Country:      rel.Country,
		Status:       rel.Status,
		ArtistCredit: rel.ArtistCredit,
		Tracks:       tracks,
	}
}
