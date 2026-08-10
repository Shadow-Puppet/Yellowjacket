package explore

import (
	"context"

	"yellowjacket/backend/autotag"
)

// AutotagClient adapts a *Service (its network MusicBrainzClient
// plus its offline dump-derived SearchIndex) to autotag.MBClient.
// Lives in the explore package so autotag stays free of
// explore-internal types — callers in app wiring construct one via
// NewAutotagClient and hand it to the scorer.
type AutotagClient struct {
	svc *Service
}

// NewAutotagClient wraps an explore Service for use by the autotag
// scorer: svc.MusicBrainz() serves the network cascade, svc's local
// SearchIndex serves the index-first pass.
func NewAutotagClient(svc *Service) *AutotagClient {
	return &AutotagClient{svc: svc}
}

// SearchReleaseGroups delegates to the wrapped client and projects
// hits into autotag's minimal shape.
func (c *AutotagClient) SearchReleaseGroups(
	ctx context.Context, query string, limit int,
) ([]autotag.MBReleaseGroupHit, int, error) {
	hits, total, err := c.svc.mb.SearchReleaseGroups(ctx, query, limit)
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

// SearchReleaseGroupsLocal searches the offline dump-derived catalog
// (explore_index) for release groups matching albumName — no network
// round-trip.  Returns ok=false when the index isn't populated yet,
// telling the resolver to rely on the network cascade alone.
func (c *AutotagClient) SearchReleaseGroupsLocal(
	ctx context.Context, albumName string, limit int,
) ([]autotag.MBReleaseGroupHit, bool) {
	if !c.svc.index.IsReady() {
		return nil, false
	}

	hits := c.svc.index.Search(ctx, albumName, limit)

	out := make([]autotag.MBReleaseGroupHit, 0, len(hits))

	for _, h := range hits {
		if h.EntityType != "release_group" {
			continue
		}

		out = append(out, autotag.MBReleaseGroupHit{
			MBID:         h.MBID,
			Title:        h.Title,
			ArtistCredit: h.ArtistName,
			FirstDate:    h.ReleaseDate,
			PrimaryType:  h.PrimaryType,
		})
	}

	return out, true
}

// BrowseReleases delegates to the wrapped client and projects each
// release (and its tracks) into autotag's shape.  Length is
// millisecond-aligned to match local audio_files.
func (c *AutotagClient) BrowseReleases(
	ctx context.Context, releaseGroupMBID string,
) ([]autotag.MBRelease, error) {
	releases, err := c.svc.mb.BrowseReleases(ctx, releaseGroupMBID)
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
	rel, err := c.svc.mb.LookupRelease(ctx, releaseMBID)
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
	rg, err := c.svc.mb.LookupReleaseGroup(ctx, releaseGroupMBID)
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

// SearchRecordings delegates to the wrapped client and projects hits
// into autotag's minimal recording shape.  Length is millisecond-
// aligned to match local audio_files.
func (c *AutotagClient) SearchRecordings(
	ctx context.Context, query string, limit int,
) ([]autotag.MBRecordingHit, int, error) {
	recs, total, err := c.svc.mb.SearchRecordings(ctx, query, limit)
	if err != nil {
		return nil, 0, err
	}

	out := make([]autotag.MBRecordingHit, 0, len(recs))
	for _, rec := range recs {
		out = append(out, autotag.MBRecordingHit{
			MBID:         rec.MBID,
			Title:        rec.Title,
			ArtistCredit: rec.ArtistCredit,
			LengthMillis: int64(rec.Length),
		})
	}

	return out, total, nil
}

// LookupRecordingReleases returns the releases a recording appears on,
// as slim references the resolver ranks to pick a representative
// release.
func (c *AutotagClient) LookupRecordingReleases(
	ctx context.Context, recordingMBID string,
) ([]autotag.MBReleaseRef, error) {
	refs, err := c.svc.mb.LookupRecordingReleases(ctx, recordingMBID)
	if err != nil {
		return nil, err
	}

	out := make([]autotag.MBReleaseRef, 0, len(refs))
	for _, r := range refs {
		out = append(out, autotag.MBReleaseRef{
			MBID:   r.MBID,
			Title:  r.Title,
			Status: r.Status,
			Date:   r.Date,
		})
	}

	return out, nil
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
