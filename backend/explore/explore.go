package explore

import (
	"context"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"yellowjacket/backend/database"
)

// Service is the Wails-bound service for the explore feature.
// It owns the lifecycle of all explore-related components: the
// MusicBrainz client, ListenBrainz client, rate limiter, and
// response cache.  Its exported methods form the binding surface
// that the frontend calls via generated TypeScript stubs.
type Service struct {
	mb         *MusicBrainzClient
	lb         *ListenBrainzClient
	cache      *Cache
	index      *SearchIndex
	artProxy   *CoverArtProxy
	artistImg  *ArtistImageProvider
	libMBID    *LibraryMBIDIndex
	caaLimiter *RateLimiter
	db         *database.DB
	logger     *slog.Logger
	ctx        context.Context
}

// NewExploreService creates a Service backed by the given
// database.  It instantiates the rate limiter, cache, MusicBrainz
// client, and ListenBrainz client internally.
func NewExploreService(logger *slog.Logger, db *database.DB) *Service {
	cache := NewCache(db, logger.WithGroup("cache"))
	lbLimiter := NewRateLimiter()
	// Cover Art Archive has its own rate limits, separate from LB.
	// Allow 8 concurrent fetches so album art loads quickly.
	caaLimiter := NewRateLimiterBurst(8, 8)
	// MB search limiter: 3 tokens/sec, burst of 1.  This spaces the
	// three concurrent search goroutines ~333ms apart instead of
	// firing all at once.  MusicBrainz uses an all-or-nothing rate
	// limit — exceeding 1/sec average causes 503 on ALL requests,
	// which triggers the library's retry loop (up to 5 × 1s waits).
	// Staggering avoids the 503 entirely while keeping total phase-1
	// latency under 1.5s (333ms stagger + ~1s MB response).
	mbSearchLimiter := NewRateLimiterBurst(3, 1)
	// MB background limiter: strict 1/sec for sustained image resolution calls.
	mbBackgroundLimiter := NewRateLimiter()
	mb := NewMusicBrainzClient(cache, mbSearchLimiter, logger.WithGroup("musicbrainz"))
	lb := NewListenBrainzClient(lbLimiter, cache, logger.WithGroup("listenbrainz"))
	artProxy := NewCoverArtProxy(db, caaLimiter)
	artistImg := NewArtistImageProvider(
		db, cache, mbBackgroundLimiter, logger.WithGroup("artist-image"),
	)
	index := NewSearchIndex(db, lb, artistImg, logger.WithGroup("search-index"))
	index.MarkReadyIfPopulated() // make index queryable immediately if data exists

	libMBID := NewLibraryMBIDIndex(db)

	logger.Info("explore service created")

	return &Service{
		mb:         mb,
		lb:         lb,
		cache:      cache,
		index:      index,
		artProxy:   artProxy,
		artistImg:  artistImg,
		libMBID:    libMBID,
		caaLimiter: caaLimiter,
		db:         db,
		logger:     logger,
		ctx:        context.Background(),
	}
}

// MusicBrainz returns the shared cached MB client so other services
// (e.g. autotag) can reuse it without spinning up a second limiter.
func (e *Service) MusicBrainz() *MusicBrainzClient {
	return e.mb
}

// CAALimiter returns the shared Cover Art Archive rate limiter.
// Consumers must respect it for any fresh CAA HTTP GETs.
func (e *Service) CAALimiter() *RateLimiter {
	return e.caaLimiter
}

// SetContext injects the Wails runtime context.  Called from
// OnStartup after the Wails runtime is initialised.
func (e *Service) SetContext(ctx context.Context) {
	e.ctx = ctx
	e.index.SetContext(ctx)
}

// StartIndexBuild kicks off the background search index build.
// Call this after the library scan completes so the indexer doesn't
// starve the scan for DB access.
func (e *Service) StartIndexBuild() {
	e.index.StartBuild(e.ctx)
}

// IndexNewArtists indexes only library artists not yet in the search
// index. Lightweight post-scan path — skips the full tier machinery.
func (e *Service) IndexNewArtists() {
	e.index.IndexNewArtists(e.ctx)
}

// StopIndexBuild cancels the background search index build.
// Call before a full rescan to free the DB for the scan.
func (e *Service) StopIndexBuild() {
	e.index.StopBuild()
}

// IsIndexReady returns true once the index has been populated.
func (e *Service) IsIndexReady() bool {
	return e.index.IsReady()
}

// WaitForIndexIdle blocks until no index build or artist indexing
// goroutine is running.  Does not cancel a running build.
func (e *Service) WaitForIndexIdle() {
	e.index.WaitForIdle()
}

// PopulateLocalCrossReferences updates the local_*_id columns on
// explore_index after a library scan.
func (e *Service) PopulateLocalCrossReferences() {
	e.index.PopulateLocalCrossReferences()
}

// GetIndexStatus returns the current search index build status.
func (e *Service) GetIndexStatus() IndexStatus {
	return e.index.GetIndexStatus()
}

// InvalidateIndexDiscographies clears the discography build
// timestamp so the next index build re-runs Tiers 2-4.  Call
// after a library rescan that may have populated new MBIDs.
func (e *Service) InvalidateIndexDiscographies() {
	e.index.InvalidateDiscographies()
}

// ---------------------------------------------------------------------------
// MusicBrainz search
// ---------------------------------------------------------------------------

// SearchArtists queries MusicBrainz for artists matching the query.
func (e *Service) SearchArtists(query string) ([]MBArtist, error) {
	artists, _, err := e.mb.SearchArtists(e.ctx, query, mbSearchLimit)

	return artists, err
}

// SearchReleaseGroups queries MusicBrainz for release groups matching the query.
func (e *Service) SearchReleaseGroups(query string) ([]MBReleaseGroup, error) {
	rgs, _, err := e.mb.SearchReleaseGroups(e.ctx, query, mbSearchLimit)

	return rgs, err
}

// SearchRecordings queries MusicBrainz for recordings matching the query.
func (e *Service) SearchRecordings(query string) ([]MBRecording, error) {
	recs, _, err := e.mb.SearchRecordings(e.ctx, query, mbSearchLimit)

	return recs, err
}

// SearchLocal queries only the local FTS5 index and returns results
// instantly with no network calls.  Returns nil if the index isn't
// ready.  The frontend calls this in parallel with Search() to show
// instant results while the full pipeline runs.
func (e *Service) SearchLocal(query string) *MBSearchResult {
	indexHits := e.index.Search(query, indexSearchLimit)
	if len(indexHits) == 0 {
		return nil
	}

	var result MBSearchResult
	mergeIndexHits(&result, indexHits)

	// Remove special-purpose artists from local results too.
	if len(result.Artists) > 0 {
		filtered := result.Artists[:0]
		for _, a := range result.Artists {
			if !mbSpecialPurposeArtists[a.MBID] {
				filtered = append(filtered, a)
			}
		}

		result.Artists = filtered
	}

	// Cap counts but skip the minBlendedScore filter — index hits
	// use scalePopularity scores that shouldn't be compared to
	// blended MB+LB scores.
	if len(result.Artists) > maxResults {
		result.Artists = result.Artists[:maxResults]
	}

	if len(result.ReleaseGroups) > maxResults {
		result.ReleaseGroups = result.ReleaseGroups[:maxResults]
	}

	if len(result.Recordings) > maxResults {
		result.Recordings = result.Recordings[:maxResults]
	}

	return &result
}

// ---------------------------------------------------------------------------
// MusicBrainz lookup
// ---------------------------------------------------------------------------

// LookupArtist fetches a single MusicBrainz artist by MBID.
// Checks the local index first — has name, type, country,
// disambiguation, sort_name for indexed artists.  Falls back to
// MB API for unknown artists and backfills the index for next time.
func (e *Service) LookupArtist(mbid string) (*MBArtist, error) {
	if indexed := e.index.LookupArtistByMBID(mbid); indexed != nil && indexed.Title != "" {
		artist := &MBArtist{
			MBID:           mbid,
			Name:           indexed.Title,
			SortName:       indexed.SortName,
			Type:           indexed.ArtistType,
			Country:        indexed.Country,
			Disambiguation: indexed.Disambiguation,
			Popularity:     indexed.Popularity,
			HasPopularity:  indexed.Popularity > 0,
			ListenerCount:  indexed.ListenerCount,
			InLibrary:      indexed.InLibrary || indexed.LocalArtistID > 0,
			LocalID:        indexed.LocalArtistID,
		}

		return artist, nil
	}

	return e.mb.LookupArtist(e.ctx, mbid)
}

// LookupReleaseGroup fetches a single MusicBrainz release group by MBID.
func (e *Service) LookupReleaseGroup(mbid string) (*MBReleaseGroup, error) {
	// Try the index first — has title, type, secondary_types, date, artist.
	if indexed := e.index.LookupReleaseGroupByMBID(mbid); indexed != nil && indexed.Title != "" {
		var secondary []string
		if indexed.SecondaryTypes != "" {
			secondary = strings.Split(indexed.SecondaryTypes, ",")
		}

		rg := &MBReleaseGroup{
			MBID:             mbid,
			Title:            indexed.Title,
			ArtistCredit:     indexed.ArtistName,
			Popularity:       indexed.Popularity,
			ListenerCount:    indexed.ListenerCount,
			PrimaryType:      indexed.PrimaryType,
			SecondaryTypes:   secondary,
			FirstReleaseDate: indexed.ReleaseDate,
			InLibrary:        indexed.InLibrary || indexed.LocalReleaseGroupID > 0,
			LocalID:          indexed.LocalReleaseGroupID,
		}

		// Background: fetch full MB data if secondary_types is empty.
		// After the first visit this will populate on the next request.
		if indexed.SecondaryTypes == "" {
			go func() {
				_, _ = e.mb.LookupReleaseGroup(e.ctx, mbid)
			}()
		}

		return rg, nil
	}

	return e.mb.LookupReleaseGroup(e.ctx, mbid)
}

// ---------------------------------------------------------------------------
// MusicBrainz browse
// ---------------------------------------------------------------------------

// BrowseReleaseGroups fetches release groups for a given artist MBID.
// Checks the local index first for instant results, then fetches from
// MusicBrainz for complete data (secondary types, precise dates).
// Also adds results to the search index (Tier 5: organic growth).
func (e *Service) BrowseReleaseGroups(artistMBID string) ([]MBReleaseGroup, error) {
	// Try the index first — returns instantly if the artist is indexed.
	if indexed := e.index.TopReleaseGroupsByArtist(artistMBID, 200); len(indexed) > 0 {
		out := make([]MBReleaseGroup, 0, len(indexed))

		// Check if ANY row has secondary types — if none do, we need
		// to refresh from MB to pick them up.  This typically happens
		// on the first visit after an artist's discography was indexed
		// from the LB top-release-groups endpoint (which doesn't
		// return secondary types).
		hasSecondaryTypes := false

		for _, r := range indexed {
			var secondary []string
			if r.SecondaryTypes != "" {
				secondary = strings.Split(r.SecondaryTypes, ",")
				hasSecondaryTypes = true
			}

			out = append(out, MBReleaseGroup{
				MBID:             r.MBID,
				Title:            r.Title,
				ArtistCredit:     r.ArtistName,
				Popularity:       r.Popularity,
				ListenerCount:    r.ListenerCount,
				PrimaryType:      r.PrimaryType,
				SecondaryTypes:   secondary,
				FirstReleaseDate: r.ReleaseDate,
				InLibrary:        r.InLibrary || r.LocalReleaseGroupID > 0,
				LocalID:          r.LocalReleaseGroupID,
			})
		}

		// Fire MB browse in background if we're missing secondary types
		// so the next visit gets them.
		if !hasSecondaryTypes {
			go func() {
				rgs, err := e.mb.BrowseReleaseGroups(e.ctx, artistMBID)
				if err == nil && len(rgs) > 0 {
					artistName := e.resolveArtistName(artistMBID, rgs)
					e.index.AddFromCache(artistName, artistMBID, rgs)
				}
			}()
		}

		return out, nil
	}

	rgs, err := e.mb.BrowseReleaseGroups(e.ctx, artistMBID)
	if err != nil {
		return nil, err
	}

	// Tier 5: organic growth — index this discography.
	artistName := e.resolveArtistName(artistMBID, rgs)

	go e.index.AddFromCache(artistName, artistMBID, rgs)

	return rgs, nil
}

// resolveArtistName picks the best available artist name for a list
// of release groups returned from MB browse-by-artist.  MB browse
// doesn't echo back the artist credit on each item (since the artist
// is the query parameter), so we need to find a name from somewhere:
//  1. First non-empty ArtistCredit on any release group
//  2. The local explore_index (if the artist was previously indexed)
//  3. A LookupArtist call to MB (last resort)
//  4. The MBID itself (worst case fallback)
func (e *Service) resolveArtistName(artistMBID string, rgs []MBReleaseGroup) string {
	// Try first non-empty ArtistCredit from the release groups.
	for _, rg := range rgs {
		if rg.ArtistCredit != "" {
			return rg.ArtistCredit
		}
	}

	// Check the index for a previously-indexed artist row.
	if indexed := e.index.LookupArtistByMBID(
		artistMBID,
	); indexed != nil && indexed.Title != "" &&
		indexed.Title != artistMBID {
		return indexed.Title
	}

	// Last resort: hit MB lookup.
	if artist, err := e.mb.LookupArtist(
		e.ctx,
		artistMBID,
	); err == nil && artist != nil &&
		artist.Name != "" {
		return artist.Name
	}

	return artistMBID
}

// BrowseReleases fetches releases for a given release group MBID.
func (e *Service) BrowseReleases(releaseGroupMBID string) ([]MBRelease, error) {
	releases, err := e.mb.BrowseReleases(e.ctx, releaseGroupMBID)
	if err != nil {
		return nil, err
	}

	// Collect all recording MBIDs across all releases and check them
	// against the local library in a single query.  Populates the
	// InLibrary flag on each track so the tracklist renderer can
	// show the library-status indicator without a per-track roundtrip.
	var trackMBIDs []string

	for _, rel := range releases {
		for _, t := range rel.Tracks {
			if t.MBID != "" {
				trackMBIDs = append(trackMBIDs, t.MBID)
			}
		}
	}

	if len(trackMBIDs) > 0 {
		found := e.libMBID.CheckMBIDs(trackMBIDs)

		for i := range releases {
			for j := range releases[i].Tracks {
				mbid := releases[i].Tracks[j].MBID
				if _, ok := found[mbid]; ok {
					releases[i].Tracks[j].InLibrary = true
				}
			}
		}
	}

	return releases, nil
}

// ---------------------------------------------------------------------------
// ListenBrainz
// ---------------------------------------------------------------------------

// TopRecordingsForArtist returns the most-listened recordings for an artist.
func (e *Service) TopRecordingsForArtist(artistMBID string) ([]LBTopRecording, error) {
	// Try the local index first (instant, no API call).
	if indexed := e.index.TopRecordingsByArtist(artistMBID, 50); len(indexed) > 0 {
		out := make([]LBTopRecording, len(indexed))
		for i, r := range indexed {
			out[i] = LBTopRecording{
				RecordingMBID:    r.MBID,
				ArtistName:       r.ArtistName,
				TrackName:        r.Title,
				TotalListenCount: r.Popularity,
				CAAReleaseMBID:   r.CAAReleaseMBID,
				ReleaseName:      r.ReleaseName,
				Length:           r.Duration,
				InLibrary:        r.InLibrary || r.LocalRecordingID > 0,
				LocalID:          r.LocalRecordingID,
			}
		}

		return out, nil
	}

	// Fall back to LB API.
	return e.lb.TopRecordingsForArtist(e.ctx, artistMBID)
}

// TopReleaseGroupsForArtist returns the most-listened release groups for an artist.
func (e *Service) TopReleaseGroupsForArtist(artistMBID string) ([]LBTopReleaseGroup, error) {
	// Try the local index first (instant, no API call).
	if indexed := e.index.TopReleaseGroupsByArtist(artistMBID, 50); len(indexed) > 0 {
		out := make([]LBTopReleaseGroup, len(indexed))
		for i, r := range indexed {
			out[i] = LBTopReleaseGroup{
				ReleaseGroupMBID: r.MBID,
				Title:            r.Title,
				ArtistName:       r.ArtistName,
				TotalListenCount: r.Popularity,
				Type:             r.PrimaryType,
				Date:             r.ReleaseDate,
				CAAReleaseMBID:   r.CAAReleaseMBID,
				InLibrary:        r.InLibrary || r.LocalReleaseGroupID > 0,
				LocalID:          r.LocalReleaseGroupID,
			}
		}

		return out, nil
	}

	// Fall back to LB API.
	return e.lb.TopReleaseGroupsForArtist(e.ctx, artistMBID)
}

// SimilarArtists returns artists similar to the given artist MBID.
func (e *Service) SimilarArtists(artistMBID string) ([]LBSimilarArtist, error) {
	// Try the pre-computed similar_artist_map first (instant, no API call).
	// This is populated during Tier 4 for library artists and their network.
	rows, err := e.db.QueryContext(`
		SELECT similar_artist_mbid, similar_artist_name, score
		FROM similar_artist_map
		WHERE source_artist_mbid = ?
		ORDER BY score DESC
	`, artistMBID)
	if err == nil {
		defer func() { _ = rows.Close() }()

		var results []LBSimilarArtist

		for rows.Next() {
			var a LBSimilarArtist
			if err := rows.Scan(&a.ArtistMBID, &a.Name, &a.Score); err == nil {
				results = append(results, a)
			}
		}

		if len(results) > 0 {
			return results, nil
		}
	}

	// Fall back to LB labs API.
	return e.lb.SimilarArtists(e.ctx, artistMBID)
}

// GetArtistPlayCount returns the total LB listen count for an artist.
// Returns 0 if unknown.
func (e *Service) GetArtistPlayCount(artistMBID string) int {
	// Try the local index first (instant).
	if pop := e.index.GetPopularity(artistMBID); pop > 0 {
		return pop
	}

	// Fall back to LB API.
	pop, err := e.lb.ArtistPopularity(e.ctx, []string{artistMBID})
	if err != nil || len(pop) == 0 {
		return 0
	}

	// Backfill index for next time.
	go e.index.BackfillPopularity(pop)

	return pop[artistMBID].ListenCount
}

// GetLibrarySimilarArtists returns similar artists to the given
// MBID that are also in the user's local library.  Uses the
// pre-computed similar_artist_map table (populated during Tier 4
// index build) joined with the artists table.  No API calls.
//
// The artists table allows multiple rows with the same MBID
// (different artist credits like "A feat. B" that resolve to the
// same MB artist), so we use EXISTS instead of JOIN to avoid
// duplicating similar_artist_map rows.
func (e *Service) GetLibrarySimilarArtists(artistMBID string) []LBSimilarArtist {
	rows, err := e.db.QueryContext(`
		SELECT s.similar_artist_mbid, s.similar_artist_name, s.score
		FROM similar_artist_map s
		WHERE s.source_artist_mbid = ?
		  AND EXISTS (
		    SELECT 1 FROM artists a
		    WHERE a.mbid = s.similar_artist_mbid
		  )
		ORDER BY s.score DESC
	`, artistMBID)
	if err != nil {
		return nil
	}

	defer func() { _ = rows.Close() }()

	var result []LBSimilarArtist

	for rows.Next() {
		var a LBSimilarArtist

		if err := rows.Scan(&a.ArtistMBID, &a.Name, &a.Score); err == nil {
			result = append(result, a)
		}
	}

	return result
}

// ---------------------------------------------------------------------------
// Cover Art Archive
// ---------------------------------------------------------------------------

// CoverArtURL returns the Cover Art Archive URL for a release's
// front cover at the default 250px size.
func (e *Service) CoverArtURL(releaseMBID string) string {
	return CoverArtURL(releaseMBID)
}

// CoverArtGroupURL returns the Cover Art Archive URL for a release
// group's front cover at the default 250px size.  This is the
// correct endpoint for search results, which return release group
// MBIDs rather than individual release MBIDs.
func (e *Service) CoverArtGroupURL(releaseGroupMBID string) string {
	return CoverArtGroupURL(releaseGroupMBID)
}

// GetThumbnail returns a base64 data URL for the release group's
// cover art.  Checks local library art first (by album+artist
// name), then disk cache, then Cover Art Archive.
// Returns "" if no cover art is available.
func (e *Service) GetThumbnail(releaseGroupMBID, albumName, artistName string) string {
	return e.artProxy.GetThumbnail(releaseGroupMBID, albumName, artistName)
}

// GetTrackThumbnail returns cover art for a track.  Accepts both
// the track's CAA release MBID and the resolved parent release
// group MBID (either may be empty).  Tries the RG first to reuse
// discography cache; falls back to the release-level CAA endpoint
// when the RG isn't known — useful when the track's preferred CAA
// release doesn't belong to any RG currently in the index.
func (e *Service) GetTrackThumbnail(
	releaseMBID, releaseGroupMBID, albumName, artistName string,
) string {
	return e.artProxy.GetTrackThumbnail(releaseMBID, releaseGroupMBID, albumName, artistName)
}

// GetCandidateThumbnail returns CAA-only cover art for an autotag
// candidate, skipping the library-by-name index so embedded ID3
// art on the user's existing files doesn't pollute the candidate
// preview.  Disk cache → network on RG → network on release.
func (e *Service) GetCandidateThumbnail(releaseMBID, releaseGroupMBID string) string {
	return e.artProxy.GetCandidateThumbnail(releaseMBID, releaseGroupMBID)
}

// TrackThumbnailRequest is a single item in a batch track thumbnail
// request.  Either ReleaseMBID or ReleaseGroupMBID may be empty;
// the proxy tries whichever is present.
type TrackThumbnailRequest struct {
	Key              string `json:"key"` // stable key used in the returned map
	ReleaseMBID      string `json:"releaseMbid"`
	ReleaseGroupMBID string `json:"releaseGroupMbid"`
	AlbumName        string `json:"albumName"`
	ArtistName       string `json:"artistName"`
}

// GetTrackThumbnails returns ONLY cached/local art for track
// requests, keyed by the caller-provided Key so callers can map
// results back to rows in their UI.
func (e *Service) GetTrackThumbnails(requests []TrackThumbnailRequest) map[string]string {
	result := make(map[string]string, len(requests))

	for _, req := range requests {
		dataURL := e.artProxy.GetTrackThumbnailCached(
			req.ReleaseMBID, req.ReleaseGroupMBID, req.AlbumName, req.ArtistName,
		)
		if dataURL != "" {
			result[req.Key] = dataURL
		}
	}

	return result
}

// ResolveReleaseGroupMBIDs takes a list of CAA release MBIDs (from
// recording metadata) and returns a map of release MBID → release
// group MBID.  The frontend uses this to fetch track cover art via
// the parent release group, reusing whatever cache exists for the
// album already.
func (e *Service) ResolveReleaseGroupMBIDs(caaReleaseMBIDs []string) map[string]string {
	return e.index.ReleaseGroupMBIDsForCAAReleaseMBIDs(caaReleaseMBIDs)
}

// ThumbnailRequest is a single item in a batch thumbnail request.
type ThumbnailRequest struct {
	MBID       string `json:"mbid"`
	AlbumName  string `json:"albumName"`
	ArtistName string `json:"artistName"`
}

// GetThumbnails fetches multiple thumbnails in one call and returns
// a map of MBID → base64 data URL.  Entries with no art are omitted.
// GetThumbnails returns ONLY cached/local art instantly — no network
// fetches.  For items missing from the cache, the frontend should
// call GetThumbnail() individually so results stream in rather than
// blocking on a batch.
func (e *Service) GetThumbnails(requests []ThumbnailRequest) map[string]string {
	result := make(map[string]string, len(requests))

	for _, req := range requests {
		dataURL := e.artProxy.GetThumbnailCached(req.MBID, req.AlbumName, req.ArtistName)
		if dataURL != "" {
			result[req.MBID] = dataURL
		}
	}

	return result
}

// GetArtistImageURL returns a base64 data URL for the artist's
// photo.  Cached on disk — first call resolves via MB/Wikidata and
// fetches from Wikimedia Commons, subsequent calls are instant.
// Returns "" if no image is available.
func (e *Service) GetArtistImageURL(artistMBID string) string {
	return e.artistImg.GetArtistImage(artistMBID)
}

// GetArtistImageCached returns a base64 data URL for the artist's
// photo ONLY if it's already on disk — no MB/Wikidata resolution
// or Wikimedia fetch.  Safe to call from library-only mode.
// Returns "" if not cached.
func (e *Service) GetArtistImageCached(artistMBID string) string {
	return e.artistImg.GetCachedImage(artistMBID)
}

// GetArtistImageCachedPath returns the asset-handler URL path for
// the artist's cached medium thumbnail, e.g.
// "/artist-images/b1/b10bbbfc-.../primary_md.jpg".  No base64, no
// network calls — just a disk existence check.  Returns "" if no
// image is cached.
func (e *Service) GetArtistImageCachedPath(artistMBID string) string {
	_, medium, _, _ := e.artistImg.GetImageURLs(artistMBID)

	return medium
}

// CheckLibraryMBIDs returns which of the given MBIDs exist in the
// local music library.  Returns a map of MBID → entity type
// ("artist", "release_group", "recording").
func (e *Service) CheckLibraryMBIDs(mbids []string) map[string]string {
	return e.libMBID.CheckMBIDs(mbids)
}

// PersonalizationResult holds popularity and personalization signals
// for a single MBID.  Exported for Wails binding.
type PersonalizationResult struct {
	Popularity      int  `json:"popularity"`
	ListenerCount   int  `json:"listenerCount"`
	InLibrary       bool `json:"inLibrary"`
	SimilarityScore int  `json:"similarityScore"`
}

// GetPopularityBatch returns LB popularity and personalization
// signals for a batch of MBIDs from the local search index.
func (e *Service) GetPopularityBatch(mbids []string) map[string]PersonalizationResult {
	batch := e.index.GetPopularityBatch(mbids)
	if batch == nil {
		return make(map[string]PersonalizationResult)
	}

	out := make(map[string]PersonalizationResult, len(batch.Popularity))
	for mbid, pop := range batch.Popularity {
		out[mbid] = PersonalizationResult{
			Popularity:      pop,
			ListenerCount:   batch.ListenerCount[mbid],
			InLibrary:       batch.InLibrary[mbid],
			SimilarityScore: batch.SimilarityScores[mbid],
		}
	}

	// Include entries that have library/similar flags but no popularity.
	for mbid := range batch.InLibrary {
		if _, ok := out[mbid]; !ok {
			out[mbid] = PersonalizationResult{
				InLibrary:       true,
				SimilarityScore: batch.SimilarityScores[mbid],
			}
		}
	}

	for mbid, score := range batch.SimilarityScores {
		if _, ok := out[mbid]; !ok {
			out[mbid] = PersonalizationResult{SimilarityScore: score}
		}
	}

	return out
}

// GetArtistMBID returns the MusicBrainz ID for a local library
// artist by name, or "" if not found or no MBID tagged.
func (e *Service) GetArtistMBID(artistName string) string {
	return e.libMBID.GetArtistMBID(artistName)
}

// GetArtistImages resolves artist images for multiple artists by
// name in one call.  Returns a map of artist name → base64 data
// URL.  Only artists with cached images are returned — no network
// fetches are triggered (use GetArtistImageURL for on-demand fetch).
func (e *Service) GetArtistImages(names []string) map[string]string {
	result := make(map[string]string, len(names))

	// Batch resolve all names → MBIDs from the library DB.
	allMBIDs := e.libMBID.AllArtistMBIDs()

	for _, name := range names {
		mbid, ok := allMBIDs[name]
		if !ok || mbid == "" {
			continue
		}

		// Only return already-cached images — don't trigger fetches.
		img := e.artistImg.GetCachedImage(mbid)
		if img != "" {
			result[name] = img
		}
	}

	return result
}

// Search concurrently queries MusicBrainz for artists, release
// groups, and recordings matching the query, then boosts results
// using ListenBrainz popularity data.  The final score blends
// text relevance (60%) with log-scaled listen counts (40%).
//
// If any sub-search or popularity lookup fails the error is logged
// and the remaining results are still returned — popularity
// failures degrade to MB-only ordering.
func (e *Service) Search(query string) (*MBSearchResult, error) {
	searchStart := time.Now()

	// Build the Lucene query: AND terms with wildcard on last.
	luceneQuery := buildLuceneQuery(query)
	// For RGs, also search by artist credit so that "queen" returns
	// albums BY Queen, not just titles containing "queen".
	rgQuery := buildLuceneQueryWithArtist(query, "releasegroup", "artist")
	// Recordings search by title only — the OR with artist caused
	// double-match inflation where tracks by "Queen" with "queen" in
	// the title got artificially boosted over more popular results.
	// The local index handles artist→recording discovery via
	// popularity-weighted FTS across title + artist_name + aliases.
	recQuery := buildLuceneQuery(query)

	e.logger.Info("search started", "query", query, "lucene", luceneQuery)

	// Phase 0: query local popularity index (instant, no API calls).
	p0Start := time.Now()
	indexHits := e.index.Search(query, indexSearchLimit) //nolint:mnd
	p0Dur := time.Since(p0Start)

	e.logger.Info("search phase 0 complete (index)",
		"query", query,
		"hits", len(indexHits),
		"elapsed", p0Dur,
	)

	// Phase 1: concurrent MB search (3 goroutines) with a deadline
	// so a slow MusicBrainz server doesn't hold up the whole search.
	//
	// First pass uses a small limit to discover total match counts.
	// If MB reports many matches, a second pass re-fetches with a
	// larger limit so the ranking pipeline has better material.
	p1Start := time.Now()

	mbCtx, mbCancel := context.WithTimeout(e.ctx, searchMBTimeout)
	defer mbCancel()

	var (
		result MBSearchResult
		mu     sync.Mutex
		wg     sync.WaitGroup
	)

	type mbInitial struct {
		artists    []MBArtist
		rgs        []MBReleaseGroup
		recordings []MBRecording
		artistN    int
		rgN        int
		recN       int
	}

	var initial mbInitial

	type searchFunc struct {
		name string
		fn   func()
	}

	searches := []searchFunc{
		{
			name: "artists",
			fn: func() {
				t := time.Now()
				artists, total, err := e.mb.SearchArtists(mbCtx, luceneQuery, mbSearchLimit)

				e.logger.Info("search MB sub-call",
					"entity", "artists",
					"elapsed", time.Since(t).Round(time.Millisecond),
					"results", len(artists),
					"totalMatches", total,
					"cached", err == nil && time.Since(t) < 5*time.Millisecond,
				)

				if err != nil {
					e.logger.Warn("search sub-call failed",
						"entity", "artists",
						"query", query,
						"error", err,
					)

					return
				}

				mu.Lock()
				initial.artists = artists
				initial.artistN = total
				mu.Unlock()
			},
		},
		{
			name: "releaseGroups",
			fn: func() {
				t := time.Now()
				rgs, total, err := e.mb.SearchReleaseGroups(mbCtx, rgQuery, mbSearchLimit)

				e.logger.Info("search MB sub-call",
					"entity", "releaseGroups",
					"elapsed", time.Since(t).Round(time.Millisecond),
					"results", len(rgs),
					"totalMatches", total,
					"cached", err == nil && time.Since(t) < 5*time.Millisecond,
				)

				if err != nil {
					e.logger.Warn("search sub-call failed",
						"entity", "releaseGroups",
						"query", query,
						"error", err,
					)

					return
				}

				mu.Lock()
				initial.rgs = rgs
				initial.rgN = total
				mu.Unlock()
			},
		},
		{
			name: "recordings",
			fn: func() {
				t := time.Now()
				recs, total, err := e.mb.SearchRecordings(mbCtx, recQuery, mbSearchLimit)

				e.logger.Info("search MB sub-call",
					"entity", "recordings",
					"elapsed", time.Since(t).Round(time.Millisecond),
					"results", len(recs),
					"totalMatches", total,
					"cached", err == nil && time.Since(t) < 5*time.Millisecond,
				)

				if err != nil {
					e.logger.Warn("search sub-call failed",
						"entity", "recordings",
						"query", query,
						"error", err,
					)

					return
				}

				mu.Lock()
				initial.recordings = recs
				initial.recN = total
				mu.Unlock()
			},
		},
	}

	wg.Add(len(searches))

	for _, s := range searches {
		go func() {
			defer wg.Done()

			s.fn()
		}()
	}

	wg.Wait()

	result.Artists = initial.artists
	result.ReleaseGroups = initial.rgs
	result.Recordings = initial.recordings

	p1Dur := time.Since(p1Start)

	e.logger.Info(
		"search phase 1 complete (MB)",
		"query",
		query,
		"artists",
		len(result.Artists),
		"releaseGroups",
		len(result.ReleaseGroups),
		"recordings",
		len(result.Recordings),
		"expanded",
		len(result.Artists) > mbSearchLimit || len(result.ReleaseGroups) > mbSearchLimit ||
			len(result.Recordings) > mbSearchLimit,
		"elapsed",
		p1Dur.Round(time.Millisecond),
	)

	// Phases 2+3: when the index is ready, use cached popularity
	// from the index to rerank MB results (no API calls).
	// When the index isn't ready, fall back to live LB API calls.
	p2Start := time.Now()
	indexReady := e.index.IsReady()

	// Phase 2a: resolve artist popularity and library membership.
	artistMBIDs := make([]string, 0, len(result.Artists))
	for _, a := range result.Artists {
		if a.MBID != "" {
			artistMBIDs = append(artistMBIDs, a.MBID)
		}
	}

	artistPop := make(map[string]int)
	libMBIDs := make(map[string]bool)
	simScores := make(map[string]int)

	if indexReady {
		// Fast path: use local index data only — no API call.
		// The batch includes popularity, in_library, and similarity scores.
		batch := e.index.GetPopularityBatch(artistMBIDs)
		if batch != nil {
			for mbid, pop := range batch.Popularity {
				artistPop[mbid] = pop
			}

			libMBIDs = batch.InLibrary
			simScores = batch.SimilarityScores
		}

		// Fill in missing artist popularity from LB synchronously
		// (with a tight timeout).  Without this, artists not yet
		// indexed get popularity 0 and the rerank can't
		// differentiate them from each other, producing nonsense
		// ordering for result sets where MB gave every candidate
		// the same text relevance score.
		var missingPop []string

		for _, mbid := range artistMBIDs {
			if artistPop[mbid] <= 0 {
				missingPop = append(missingPop, mbid)
			}
		}

		if len(missingPop) > 0 {
			popCtx, popCancel := context.WithTimeout(e.ctx, searchSlowPathTimeout)

			pop, err := e.lb.ArtistPopularity(popCtx, missingPop)

			popCancel()

			if err == nil && pop != nil {
				for mbid, data := range pop {
					if data.ListenCount > 0 {
						artistPop[mbid] = data.ListenCount
					}
				}

				go e.index.BackfillPopularity(pop)
			}
		}
	} else {
		// Slow path: fetch from LB API with a tight timeout
		// so a hung LB server doesn't stall the search.
		popCtx, popCancel := context.WithTimeout(e.ctx, 2*time.Second)
		pop, _ := e.lb.ArtistPopularity(popCtx, artistMBIDs)

		popCancel()

		if pop != nil {
			artistPop = listenCounts(pop)

			go e.index.BackfillPopularity(pop)
		}

		// Still need library/similar membership from the index.
		batch := e.index.GetPopularityBatch(artistMBIDs)
		if batch != nil {
			libMBIDs = batch.InLibrary
			simScores = batch.SimilarityScores
		}
	}

	// Mark popularity and library status on artists for downstream use.
	for i := range result.Artists {
		if pop, ok := artistPop[result.Artists[i].MBID]; ok && pop > 0 {
			result.Artists[i].HasPopularity = true
			result.Artists[i].Popularity = pop
		}

		if libMBIDs[result.Artists[i].MBID] {
			result.Artists[i].InLibrary = true
		}
	}

	rerankArtistsPersonalized(result.Artists, artistPop, libMBIDs, simScores)

	// Phase 2b: rerank release groups and recordings.
	if indexReady {
		e.boostWithIndexPopularityRGsAndRecs(&result)
	} else {
		// Slow path: LB popularity + cross-reference in parallel.
		slowCtx, slowCancel := context.WithTimeout(e.ctx, searchSlowPathTimeout)

		var wgSlow sync.WaitGroup
		wgSlow.Add(2) //nolint:mnd

		// Leg 1: LB popularity for RGs and recordings.
		go func() {
			defer wgSlow.Done()

			e.boostWithPopularityRGsAndRecs(&result)
		}()

		// Leg 2: cross-reference artist discographies.
		go func() {
			defer wgSlow.Done()

			if slowCtx.Err() == nil {
				e.crossReferenceAlbums(slowCtx, query, &result)
			}
		}()

		wgSlow.Wait()
		slowCancel()
	}

	p2Dur := time.Since(p2Start)

	e.logger.Info("search phase 2-3 complete (rerank)",
		"query", query,
		"indexReady", indexReady,
		"elapsed", p2Dur.Round(time.Millisecond),
	)

	// Phase 4: merge local index hits into results, dedup by MBID.
	mergeIndexHits(&result, indexHits)

	// Phase 5: boost exact/substring name matches so a search for
	// "the teenagers" ranks "The Teenagers" above "The Beatles"
	// even when The Beatles have vastly more listens.
	e.boostNameMatches(query, &result)

	// Phase 6: filter low-scoring results and cap counts.
	filterAndCap(&result)

	// Phase 7: resolve top result cards via intent scoring.
	result.TopResults = e.resolveTopResults(query, &result)

	totalDur := time.Since(searchStart)

	e.logger.Info("search completed",
		"query", query,
		"artists", len(result.Artists),
		"releaseGroups", len(result.ReleaseGroups),
		"recordings", len(result.Recordings),
		"total", totalDur.Round(time.Millisecond),
		"phase0", p0Dur.Round(time.Millisecond),
		"phase1_mb", p1Dur.Round(time.Millisecond),
		"phase2_rerank", p2Dur.Round(time.Millisecond),
	)

	return &result, nil
}

// ---------------------------------------------------------------------------
// Cross-reference search
// ---------------------------------------------------------------------------

const (
	// crossRefArtists is the number of top artists whose
	// discographies are searched for matching albums.
	crossRefArtists = 3

	// crossRefMinRatio is the minimum fuzzy match ratio (0–1)
	// for an album title to be considered a match.
	crossRefMinRatio = 0.4
)

// crossReferenceAlbums browses the discographies of the top N
// artists and fuzzy-matches the query against album titles.
// Matched albums not already in result.ReleaseGroups are injected
// at the front.  This handles queries like "for you tatsuro"
// where MB text search can't associate the title with the artist.
func (e *Service) crossReferenceAlbums(ctx context.Context, query string, result *MBSearchResult) {
	if len(result.Artists) == 0 {
		return
	}

	limit := crossRefArtists
	if limit > len(result.Artists) {
		limit = len(result.Artists)
	}

	topArtists := result.Artists[:limit]
	queryLower := strings.ToLower(strings.TrimSpace(query))

	// Build a set of release group MBIDs already in results.
	existing := make(map[string]bool, len(result.ReleaseGroups))
	for _, rg := range result.ReleaseGroups {
		existing[rg.MBID] = true
	}

	// Browse discographies concurrently.
	type match struct {
		rg    MBReleaseGroup
		ratio float64
	}

	var (
		matches []match
		mu      sync.Mutex
		wg      sync.WaitGroup
	)

	wg.Add(limit)

	for _, artist := range topArtists {
		go func(a MBArtist) {
			defer wg.Done()

			rgs, err := e.mb.BrowseReleaseGroups(ctx, a.MBID)
			if err != nil {
				e.logger.Warn("cross-reference browse failed",
					"artist", a.Name,
					"mbid", a.MBID,
					"error", err,
				)

				return
			}

			for _, rg := range rgs {
				if existing[rg.MBID] {
					continue
				}

				ratio := fuzzyMatchRatio(queryLower, strings.ToLower(rg.Title))
				if ratio >= crossRefMinRatio {
					mu.Lock()

					matches = append(matches, match{rg: rg, ratio: ratio})

					mu.Unlock()
				}
			}
		}(artist)
	}

	wg.Wait()

	if len(matches) == 0 {
		return
	}

	// Sort by match ratio descending.
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].ratio > matches[j].ratio
	})

	// Inject at the front of release groups.
	injected := make([]MBReleaseGroup, 0, len(matches))

	for _, m := range matches {
		if !existing[m.rg.MBID] {
			injected = append(injected, m.rg)
			existing[m.rg.MBID] = true
		}
	}

	if len(injected) > 0 {
		result.ReleaseGroups = append(injected, result.ReleaseGroups...)

		e.logger.Info("cross-reference injected albums",
			"count", len(injected),
			"topMatch", injected[0].Title,
		)
	}
}

// fuzzyMatchRatio computes a similarity score between query and
// title.  It checks:
//  1. Whether the title appears as a substring of the query (or
//     vice versa) — handles "for you tatsuro" containing "for you"
//  2. Word overlap ratio as a fallback
//
// Returns 0–1 where 1 is a perfect match.
func fuzzyMatchRatio(query, title string) float64 {
	if query == title {
		return 1.0
	}

	// Substring containment: "for you tatsuro" contains "for you".
	// Use both character ratio and word ratio, take the higher one.
	if strings.Contains(query, title) || strings.Contains(title, query) {
		shorter := len(title)
		longer := len(query)

		if shorter > longer {
			shorter, longer = longer, shorter
		}

		charRatio := float64(shorter) / float64(longer)

		// Also check word-level ratio for short titles in long queries.
		titleWords := strings.Fields(title)
		queryWords := strings.Fields(query)

		wordRatio := float64(len(titleWords)) / float64(len(queryWords))
		if len(titleWords) > len(queryWords) {
			wordRatio = float64(len(queryWords)) / float64(len(titleWords))
		}

		if wordRatio > charRatio {
			return wordRatio
		}

		return charRatio
	}

	// Word overlap: count how many query words appear in the title.
	queryWords := strings.Fields(query)
	titleWords := strings.Fields(title)

	if len(queryWords) == 0 || len(titleWords) == 0 {
		return 0
	}

	titleSet := make(map[string]bool, len(titleWords))
	for _, w := range titleWords {
		titleSet[w] = true
	}

	hits := 0

	for _, w := range queryWords {
		if titleSet[w] {
			hits++
		}
	}

	return float64(hits) / float64(len(queryWords))
}

// ---------------------------------------------------------------------------
// Index result merging
// ---------------------------------------------------------------------------

// mergeIndexHits injects local popularity index results into the
// MBSearchResult.  Index hits for entity types not already present
// (by MBID) are prepended so they appear first — they come from
// the most popular albums/tracks globally and deserve prominence.
func mergeIndexHits(result *MBSearchResult, hits []SearchIndexResult) {
	if len(hits) == 0 {
		return
	}

	// Build MBID sets for existing results.
	artistMBIDs := make(map[string]bool, len(result.Artists))
	for _, a := range result.Artists {
		artistMBIDs[a.MBID] = true
	}

	rgMBIDs := make(map[string]bool, len(result.ReleaseGroups))
	for _, rg := range result.ReleaseGroups {
		rgMBIDs[rg.MBID] = true
	}

	recMBIDs := make(map[string]bool, len(result.Recordings))
	for _, r := range result.Recordings {
		recMBIDs[r.MBID] = true
	}

	// Collect new entries from index that MB didn't return.
	var (
		newArtists []MBArtist
		newRGs     []MBReleaseGroup
		newRecs    []MBRecording
	)

	for _, h := range hits {
		switch h.EntityType {
		case "artist":
			if !artistMBIDs[h.MBID] {
				score := int(float64(scalePopularity(h.Popularity)) * 0.5)

				newArtists = append(newArtists, MBArtist{
					MBID:           h.MBID,
					Name:           h.Title,
					Type:           h.ArtistType,
					Country:        h.Country,
					Disambiguation: h.Disambiguation,
					SortName:       h.SortName,
					Score:          score,
					HasPopularity:  h.Popularity > 0,
					Popularity:     h.Popularity,
					ListenerCount:  h.ListenerCount,
					InLibrary:      h.InLibrary || h.LocalArtistID > 0,
					LocalID:        h.LocalArtistID,
				})

				artistMBIDs[h.MBID] = true
			}

		case "release_group":
			if !rgMBIDs[h.MBID] {
				score := int(float64(scalePopularity(h.Popularity)) * 0.5)

				var secondary []string
				if h.SecondaryTypes != "" {
					secondary = strings.Split(h.SecondaryTypes, ",")
				}

				newRGs = append(newRGs, MBReleaseGroup{
					MBID:             h.MBID,
					Title:            h.Title,
					ArtistCredit:     h.ArtistName,
					Score:            score,
					Popularity:       h.Popularity,
					ListenerCount:    h.ListenerCount,
					PrimaryType:      h.PrimaryType,
					SecondaryTypes:   secondary,
					FirstReleaseDate: h.ReleaseDate,
					InLibrary:        h.InLibrary || h.LocalReleaseGroupID > 0,
					LocalID:          h.LocalReleaseGroupID,
				})
				rgMBIDs[h.MBID] = true
			}

		case "recording":
			if !recMBIDs[h.MBID] {
				score := int(float64(scalePopularity(h.Popularity)) * 0.5)

				newRecs = append(newRecs, MBRecording{
					MBID:          h.MBID,
					Title:         h.Title,
					Length:        h.Duration,
					ArtistCredit:  h.ArtistName,
					Score:         score,
					Popularity:    h.Popularity,
					ListenerCount: h.ListenerCount,
					InLibrary:     h.InLibrary || h.LocalRecordingID > 0,
					LocalID:       h.LocalRecordingID,
				})

				recMBIDs[h.MBID] = true
			}
		}
	}

	// Prepend index hits so they appear before MB-only results.
	// The subsequent reranking and filtering passes will sort
	// everything by blended score.
	if len(newArtists) > 0 {
		result.Artists = append(newArtists, result.Artists...)
	}

	if len(newRGs) > 0 {
		result.ReleaseGroups = append(newRGs, result.ReleaseGroups...)
	}

	if len(newRecs) > 0 {
		result.Recordings = append(newRecs, result.Recordings...)
	}
}

// scalePopularity maps a raw LB listen count to a 0–100 score
// comparable with MB/blended scores.  Uses log scaling.
func scalePopularity(listens int) int {
	if listens <= 0 {
		return 0
	}

	// log10(1M) ≈ 6, log10(10M) ≈ 7.  Scale so 1M+ listens → ~80-100.
	const scale = 15.0 // tuned so ~100K listens → ~75, ~1M → ~90

	score := int(math.Log10(float64(listens)) * scale)
	if score > 100 { //nolint:mnd
		score = 100
	}

	return score
}

// ---------------------------------------------------------------------------
// Filtering and capping
// ---------------------------------------------------------------------------

func filterAndCap(result *MBSearchResult) {
	// Filter artists: remove SPAs, low-scoring results, and
	// low-popularity garbage when better alternatives exist.
	if len(result.Artists) > 0 {
		filtered := result.Artists[:0]

		// Find the max popularity among artists to calibrate the
		// garbage threshold.  If ANY artist has real popularity,
		// suppress zero-popularity results.
		maxPop := 0
		for _, a := range result.Artists {
			if a.Popularity > maxPop {
				maxPop = a.Popularity
			}
		}

		for _, a := range result.Artists {
			if mbSpecialPurposeArtists[a.MBID] {
				continue
			}

			if a.Score < minBlendedScore {
				continue
			}

			// Drop very-low-popularity results when the result
			// set contains meaningfully popular alternatives.
			if maxPop >= minPopularityFloor && a.HasPopularity &&
				a.Popularity < minPopularityFloor {
				continue
			}

			filtered = append(filtered, a)
		}

		result.Artists = filtered
	}

	// Filter release groups by minimum blended score + popularity floor.
	if len(result.ReleaseGroups) > 0 {
		filtered := result.ReleaseGroups[:0]

		for _, r := range result.ReleaseGroups {
			if r.Score >= minBlendedScore {
				filtered = append(filtered, r)
			}
		}

		result.ReleaseGroups = filtered
	}

	// Filter recordings by minimum blended score.
	if len(result.Recordings) > 0 {
		filtered := result.Recordings[:0]

		for _, r := range result.Recordings {
			if r.Score >= minBlendedScore {
				filtered = append(filtered, r)
			}
		}

		result.Recordings = filtered
	}

	// Cap each slice.
	if len(result.Artists) > maxResults {
		result.Artists = result.Artists[:maxResults]
	}

	if len(result.ReleaseGroups) > maxResults {
		result.ReleaseGroups = result.ReleaseGroups[:maxResults]
	}

	if len(result.Recordings) > maxResults {
		result.Recordings = result.Recordings[:maxResults]
	}
}

// ---------------------------------------------------------------------------
// Popularity-boosted reranking
// ---------------------------------------------------------------------------

const (
	// mbSearchLimit is the initial limit passed to each MB search call.
	// The pipeline may re-fetch with a larger limit (up to mbSearchMaxLimit)
	// when MB reports many total matches.
	mbSearchLimit = 25

	// mbSearchMaxLimit caps the expanded fetch.  MB's API maximum is 100.
	mbSearchMaxLimit = 75 //nolint:unused // referenced by deferred MB search rework

	// indexSearchLimit is the number of results to fetch from the local
	// popularity index (Phase 0).  Larger than maxResults because
	// results are filtered and the index is the primary search domain.
	indexSearchLimit = 60

	// searchMBTimeout is the maximum time to wait for MusicBrainz
	// API responses during interactive search.  If MB is slow,
	// results degrade to index-only rather than blocking the user.
	searchMBTimeout = 3 * time.Second

	// searchSlowPathTimeout caps the total time spent on the slow
	// path (LB popularity + cross-referencing).  When the index
	// isn't ready, these API calls can stack up — especially
	// cross-referencing, which browses 3 artist discographies via
	// MB and can hit 429 retries.  The timeout ensures search
	// returns within a reasonable window.
	searchSlowPathTimeout = 3 * time.Second

	// maxResults caps each entity slice after filtering.
	maxResults = 15

	// minBlendedScore is the absolute floor — no result survives
	// below this regardless of popularity.
	minBlendedScore = 15

	// minPopularityFloor is the minimum popularity required when
	// higher-popularity alternatives exist.  Results below this
	// threshold are dropped unless every result in that entity type
	// is below it (to avoid empty results for niche queries).
	minPopularityFloor = 50

	relevanceWeight       = 0.35
	popularityWeight      = 0.50
	personalizationWeight = 0.15

	// Personalization signal values (0.0–1.0).
	personalInLibrary = 1.0
	personalSimilar   = 0.5
)

// tierBonus maps artist name-match tiers to percentage score multipliers.
// Applied as: score = score * (1 + multiplier).  The spread is aggressive:
// close matches get amplified so popularity can dominate among them,
// while distant matches get heavily penalized to suppress garbage.
//
//nolint:gochecknoglobals
var tierBonus = map[int]float64{
	0: 0.25,  // exact match: +25%
	1: 0.15,  // starts with: +15%
	2: -0.10, // substring (query buried in name): -10%
	3: -0.30, // no substring match: -30%
}

// rgTierBonus maps release group match tiers to percentage multipliers.
// More aggressive spread to suppress results that match neither title
// nor artist credit.
//
//nolint:gochecknoglobals
var rgTierBonus = map[int]float64{
	0: 0.20,  // artist credit exact match: +20%
	1: 0.12,  // artist credit contains query: +12%
	2: 0.05,  // title exact match: +5%
	3: 0.0,   // title contains query: no change
	4: -0.25, // no match: -25%
}

// mbSpecialPurposeArtists is a set of MusicBrainz Special Purpose
// Artist MBIDs that should be excluded from search results.  These
// are placeholder entries (e.g. [unknown], [anonymous]) that
// accumulate thousands of recordings and artificially high
// popularity, polluting search results.
//
// See: https://musicbrainz.org/doc/Style/Unknown_and_untitled/Special_purpose_artist
//
//nolint:gochecknoglobals
var mbSpecialPurposeArtists = map[string]bool{
	"125ec42a-7229-4250-afc5-e057484327fe": true, // [unknown]
	"f731ccc4-e22a-43af-a747-64213f8768e7": true, // [anonymous]
	"33cf029c-63b0-41a0-9855-be2a3665fb3b": true, // [data]
	"314e1c25-dde7-4e4d-b2f4-0a7b9f7c56dc": true, // [dialogue]
	"eec63d3c-3b81-4ad4-b1e4-7c147c4d2b61": true, // [no artist]
	"9be7f096-97ec-4615-8957-8c3b659f51b4": true, // [traditional]
	"80a8851f-444c-4539-892b-ad2a49f7f0d0": true, // [Church bells]
	"ae636985-40e8-4fe2-80cb-9c1a21c6e30a": true, // Various Artists (SPA, accumulates bogus popularity)
	"89ad4ac3-39f7-470e-963a-56509c546377": true, // Various Artists (regular MBID, same issue)
}

// boostWithIndexPopularity reranks MB search results using
// popularity data from the local search index.  No API calls —
// just SQLite lookups.  This is the fast path used when the index
// is ready.
//
//nolint:unused // referenced by deferred MB search rework.
func (e *Service) boostWithIndexPopularity(result *MBSearchResult) {
	// Collect all MBIDs across all entity types.
	allMBIDs := make([]string, 0,
		len(result.Artists)+len(result.ReleaseGroups)+len(result.Recordings))

	for _, a := range result.Artists {
		if a.MBID != "" {
			allMBIDs = append(allMBIDs, a.MBID)
		}
	}

	for _, rg := range result.ReleaseGroups {
		if rg.MBID != "" {
			allMBIDs = append(allMBIDs, rg.MBID)
		}
	}

	for _, r := range result.Recordings {
		if r.MBID != "" {
			allMBIDs = append(allMBIDs, r.MBID)
		}
	}

	// Single batch query for all popularity + in_library data.
	batch := e.index.GetPopularityBatch(allMBIDs)
	if batch == nil {
		return
	}

	// Build per-entity maps from the batch result.
	artistPop := make(map[string]int, len(result.Artists))
	for i, a := range result.Artists {
		if pop, ok := batch.Popularity[a.MBID]; ok {
			artistPop[a.MBID] = pop
			result.Artists[i].HasPopularity = true
			result.Artists[i].Popularity = pop
		}

		if batch.InLibrary[a.MBID] {
			result.Artists[i].InLibrary = true
		}
	}

	rerankArtistsPersonalized(result.Artists, artistPop, batch.InLibrary, batch.SimilarityScores)

	rgPop := make(map[string]int, len(result.ReleaseGroups))
	for i, rg := range result.ReleaseGroups {
		if pop, ok := batch.Popularity[rg.MBID]; ok {
			rgPop[rg.MBID] = pop
			result.ReleaseGroups[i].Popularity = pop
		}

		if batch.InLibrary[rg.MBID] {
			result.ReleaseGroups[i].InLibrary = true
		}
	}

	rerankReleaseGroupsPersonalized(
		result.ReleaseGroups,
		rgPop,
		batch.InLibrary,
		batch.SimilarityScores,
	)

	recPop := make(map[string]int, len(result.Recordings))
	for i, r := range result.Recordings {
		if pop, ok := batch.Popularity[r.MBID]; ok {
			recPop[r.MBID] = pop
			result.Recordings[i].Popularity = pop
		}

		if batch.InLibrary[r.MBID] {
			result.Recordings[i].InLibrary = true
		}
	}

	rerankRecordingsPersonalized(result.Recordings, recPop, batch.InLibrary, batch.SimilarityScores)
}

// boostWithIndexPopularityRGsAndRecs reranks release groups and
// recordings using index popularity.  Artists are handled separately
// via the always-on LB API lookup.
func (e *Service) boostWithIndexPopularityRGsAndRecs(result *MBSearchResult) {
	allMBIDs := make([]string, 0,
		len(result.ReleaseGroups)+len(result.Recordings))

	for _, rg := range result.ReleaseGroups {
		if rg.MBID != "" {
			allMBIDs = append(allMBIDs, rg.MBID)
		}
	}

	for _, r := range result.Recordings {
		if r.MBID != "" {
			allMBIDs = append(allMBIDs, r.MBID)
		}
	}

	if len(allMBIDs) == 0 {
		return
	}

	batch := e.index.GetPopularityBatch(allMBIDs)
	if batch == nil {
		batch = &PopularityBatchResult{
			Popularity: map[string]int{},
			InLibrary:  map[string]bool{},
		}
	}

	// Collect MBIDs the index had no popularity for.  For result sets
	// where every candidate has identical MB relevance (e.g. many
	// covers of the same song), missing popularity means the rerank
	// has no signal to pick between them — so fall back to the LB
	// popularity API for just the missing entries.  This keeps the
	// common path cache-only while correctness-critical cases get
	// a ~1 round-trip to LB.
	missingRecs := make([]string, 0)

	for _, r := range result.Recordings {
		if r.MBID == "" {
			continue
		}

		if _, ok := batch.Popularity[r.MBID]; !ok {
			missingRecs = append(missingRecs, r.MBID)
		}
	}

	missingRGs := make([]string, 0)

	for _, rg := range result.ReleaseGroups {
		if rg.MBID == "" {
			continue
		}

		if _, ok := batch.Popularity[rg.MBID]; !ok {
			missingRGs = append(missingRGs, rg.MBID)
		}
	}

	if len(missingRecs) > 0 || len(missingRGs) > 0 {
		e.fillMissingPopularity(batch, missingRecs, missingRGs)
	}

	rgPop := make(map[string]int, len(result.ReleaseGroups))
	for i, rg := range result.ReleaseGroups {
		if pop, ok := batch.Popularity[rg.MBID]; ok {
			rgPop[rg.MBID] = pop
			result.ReleaseGroups[i].Popularity = pop
		}

		if batch.InLibrary[rg.MBID] {
			result.ReleaseGroups[i].InLibrary = true
		}
	}

	rerankReleaseGroups(result.ReleaseGroups, rgPop)

	recPop := make(map[string]int, len(result.Recordings))
	for i, r := range result.Recordings {
		if pop, ok := batch.Popularity[r.MBID]; ok {
			recPop[r.MBID] = pop
			result.Recordings[i].Popularity = pop
		}

		if batch.InLibrary[r.MBID] {
			result.Recordings[i].InLibrary = true
		}
	}

	rerankRecordings(result.Recordings, recPop)
}

// fillMissingPopularity fetches LB popularity for recordings and
// release groups that weren't in the local index, merging the
// results back into batch.Popularity.  Also backfills the index in
// the background so subsequent searches hit the cache.  Runs the
// two LB POST calls concurrently and bounds the total wait to
// searchSlowPathTimeout so a slow LB response can't block search.
func (e *Service) fillMissingPopularity(
	batch *PopularityBatchResult,
	missingRecs []string,
	missingRGs []string,
) {
	ctx, cancel := context.WithTimeout(e.ctx, searchSlowPathTimeout)
	defer cancel()

	var (
		recPop map[string]PopularityData
		rgPop  map[string]PopularityData
		wg     sync.WaitGroup
	)

	if len(missingRecs) > 0 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			pop, err := e.lb.RecordingPopularity(ctx, missingRecs)
			if err != nil {
				e.logger.Debug("search: fill missing recording popularity failed",
					"count", len(missingRecs), "error", err)

				return
			}

			recPop = pop
		}()
	}

	if len(missingRGs) > 0 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			pop, err := e.lb.ReleaseGroupPopularity(ctx, missingRGs)
			if err != nil {
				e.logger.Debug("search: fill missing RG popularity failed",
					"count", len(missingRGs), "error", err)

				return
			}

			rgPop = pop
		}()
	}

	wg.Wait()

	// Merge LB results into the batch map so the subsequent rerank
	// picks them up without needing a second lookup path.
	for mbid, data := range recPop {
		batch.Popularity[mbid] = data.ListenCount
		if batch.ListenerCount != nil {
			batch.ListenerCount[mbid] = data.ListenerCount
		}
	}

	for mbid, data := range rgPop {
		batch.Popularity[mbid] = data.ListenCount
		if batch.ListenerCount != nil {
			batch.ListenerCount[mbid] = data.ListenerCount
		}
	}

	// Backfill the index in the background so next time this query
	// runs, the index has the answer and we skip the LB round-trip.
	if len(recPop) > 0 {
		go e.index.BackfillPopularity(recPop)
	}

	if len(rgPop) > 0 {
		go e.index.BackfillPopularity(rgPop)
	}
}

// boostWithPopularityRGsAndRecs fetches LB popularity for release
// groups and recordings only (artist popularity is fetched separately
// in the main search path).  Runs two concurrent POST calls.
func (e *Service) boostWithPopularityRGsAndRecs(result *MBSearchResult) {
	recordingMBIDs := make([]string, len(result.Recordings))
	for i, r := range result.Recordings {
		recordingMBIDs[i] = r.MBID
	}

	rgMBIDs := make([]string, len(result.ReleaseGroups))
	for i, rg := range result.ReleaseGroups {
		rgMBIDs[i] = rg.MBID
	}

	// Fetch popularity concurrently (2 POST calls).
	var (
		recordingPopData map[string]PopularityData
		rgPopData        map[string]PopularityData
		wg               sync.WaitGroup
	)

	wg.Add(2) //nolint:mnd

	go func() {
		defer wg.Done()

		pop, err := e.lb.RecordingPopularity(e.ctx, recordingMBIDs)
		if err != nil {
			e.logger.Warn("popularity lookup failed", "entity", "recording", "error", err)

			return
		}

		recordingPopData = pop
	}()

	go func() {
		defer wg.Done()

		pop, err := e.lb.ReleaseGroupPopularity(e.ctx, rgMBIDs)
		if err != nil {
			e.logger.Warn("popularity lookup failed", "entity", "releaseGroup", "error", err)

			return
		}

		rgPopData = pop
	}()

	wg.Wait()

	// Backfill index with popularity data for future searches.
	if recordingPopData != nil {
		go e.index.BackfillPopularity(recordingPopData)
	}

	if rgPopData != nil {
		go e.index.BackfillPopularity(rgPopData)
	}

	rerankRecordings(result.Recordings, listenCounts(recordingPopData))
	rerankReleaseGroups(result.ReleaseGroups, listenCounts(rgPopData))
}

// boostNameMatches re-sorts artists and release groups so that
// exact or substring name matches rank above results that only
// matched on common words like "the".  Without this, a search
// for "the teenagers" would rank The Beatles above The Teenagers
// because The Beatles' massive popularity compensates for their
// weak text relevance on the word "the".
//
// The boost is applied after popularity reranking so it acts as
// a final tiebreaker that respects user intent.
func (e *Service) boostNameMatches(query string, result *MBSearchResult) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return
	}

	// Apply tier multiplier to artist scores.  Percentage-based so the
	// boost scales with the artist's existing score — a popular
	// near-match can overcome an unpopular exact match when the
	// popularity gap is proportionally larger than the tier difference.
	if len(result.Artists) > 1 {
		for i := range result.Artists {
			tier := nameMatchTier(q, strings.ToLower(result.Artists[i].Name))
			result.Artists[i].Score = int(
				float64(result.Artists[i].Score) * (1.0 + tierBonus[tier]),
			)
		}

		sort.SliceStable(result.Artists, func(i, j int) bool {
			return result.Artists[i].Score > result.Artists[j].Score
		})

		// For same-named artists in tier 0, resolve ordering via
		// a targeted LB popularity lookup.
		e.disambiguateSameNameArtists(q, result.Artists)
	}

	// Apply tier multiplier to release group scores.
	if len(result.ReleaseGroups) > 1 {
		for i := range result.ReleaseGroups {
			tier := rgMatchTier(q,
				strings.ToLower(result.ReleaseGroups[i].Title),
				strings.ToLower(result.ReleaseGroups[i].ArtistCredit))
			result.ReleaseGroups[i].Score = int(
				float64(result.ReleaseGroups[i].Score) * (1.0 + rgTierBonus[tier]),
			)
		}

		sort.SliceStable(result.ReleaseGroups, func(i, j int) bool {
			return result.ReleaseGroups[i].Score > result.ReleaseGroups[j].Score
		})
	}
}

// disambiguateSameNameArtists resolves ordering among artists
// that share the exact same name as the query by fetching their
// LB popularity.  This is a targeted micro-lookup (typically 2-6
// MBIDs) that only fires when the index fast path couldn't
// meaningfully differentiate same-named artists.
func (e *Service) disambiguateSameNameArtists(query string, artists []MBArtist) {
	// Find the contiguous block of tier-0 same-name artists at the front.
	var sameNameEnd int

	for sameNameEnd < len(artists) {
		if strings.ToLower(artists[sameNameEnd].Name) != query {
			break
		}

		sameNameEnd++
	}

	if sameNameEnd < 2 {
		return // 0 or 1 same-name artists — nothing to disambiguate
	}

	// Collect MBIDs for the targeted LB lookup.
	mbids := make([]string, 0, sameNameEnd)
	for i := range sameNameEnd {
		if artists[i].MBID != "" {
			mbids = append(mbids, artists[i].MBID)
		}
	}

	if len(mbids) < 2 {
		return
	}

	pop, err := e.lb.ArtistPopularity(e.ctx, mbids)
	if err != nil || len(pop) == 0 {
		return
	}

	// Re-sort the same-name block by LB popularity descending.
	sort.SliceStable(artists[:sameNameEnd], func(i, j int) bool {
		return pop[artists[i].MBID].ListenCount > pop[artists[j].MBID].ListenCount
	})
}

// nameMatchTier returns a tier value for how well a name matches
// the query.  Lower is better:
//
//	0 = exact match ("the teenagers" == "the teenagers")
//	1 = name starts with query ("the teenagers" in "the teenagers feat. X")
//	2 = query is a substring ("the teenagers" in "al supersonic & the teenagers")
//	3 = no substring match (only individual words matched)
func nameMatchTier(query, name string) int {
	if name == query {
		return 0
	}

	if strings.HasPrefix(name, query) {
		return 1
	}

	if strings.Contains(name, query) {
		return 2
	}

	return 3
}

// rgMatchTier returns a tier for release groups considering both
// the title and artist credit.  An album by "Hop Along" called
// "Painted Shut" should rank above a tribute album called
// "A Hop Along Tribute" by Various Artists.
//
//	0 = artist credit matches query exactly ("hop along" == "hop along")
//	1 = artist credit starts with or contains query
//	2 = title matches query exactly
//	3 = title starts with or contains query
//	4 = no match in either field
func rgMatchTier(query, title, artistCredit string) int {
	// Artist credit match is stronger — it means the album is BY
	// the searched artist, not just mentioning them in the title.
	if artistCredit == query {
		return 0
	}

	if strings.Contains(artistCredit, query) {
		return 1
	}

	// Title match — the album name contains the query.
	if title == query {
		return 2
	}

	if strings.Contains(title, query) {
		return 3
	}

	return 4
}

// rerankArtists sorts artists by blended score and updates their
// Score field to the new value (0–100 scale).
//
//nolint:unused // referenced by deferred MB search rework.
func rerankArtists(artists []MBArtist, pop map[string]int, libraryMBIDs map[string]bool) {
	rerankArtistsPersonalized(artists, pop, libraryMBIDs, nil)
}

func rerankArtistsPersonalized(
	artists []MBArtist,
	pop map[string]int,
	inLib map[string]bool,
	simScores map[string]int,
) {
	if len(artists) == 0 {
		return
	}

	maxPop := maxListenCount(pop)
	maxSim := maxSimScoreVal(simScores)

	sort.SliceStable(artists, func(i, j int) bool {
		si := blendedScoreFull(
			float64(artists[i].Score)/100.0,
			pop[artists[i].MBID],
			maxPop,
			personalScore(artists[i].MBID, inLib, simScores, maxSim),
		)
		sj := blendedScoreFull(
			float64(artists[j].Score)/100.0,
			pop[artists[j].MBID],
			maxPop,
			personalScore(artists[j].MBID, inLib, simScores, maxSim),
		)

		return si > sj
	})

	for i := range artists {
		s := blendedScoreFull(
			float64(artists[i].Score)/100.0,
			pop[artists[i].MBID],
			maxPop,
			personalScore(artists[i].MBID, inLib, simScores, maxSim),
		)
		artists[i].Score = int(s * 100)
	}
}

// rerankRecordings sorts recordings by blended score and updates
// their Score field.
func rerankRecordings(recordings []MBRecording, pop map[string]int) {
	rerankRecordingsPersonalized(recordings, pop, nil, nil)
}

func rerankRecordingsPersonalized(
	recordings []MBRecording,
	pop map[string]int,
	inLib map[string]bool,
	simScores map[string]int,
) {
	if len(recordings) == 0 {
		return
	}

	maxPop := maxListenCount(pop)
	maxSim := maxSimScoreVal(simScores)

	sort.SliceStable(recordings, func(i, j int) bool {
		si := blendedScoreFull(
			float64(recordings[i].Score)/100.0,
			pop[recordings[i].MBID],
			maxPop,
			personalScore(recordings[i].MBID, inLib, simScores, maxSim),
		)
		sj := blendedScoreFull(
			float64(recordings[j].Score)/100.0,
			pop[recordings[j].MBID],
			maxPop,
			personalScore(recordings[j].MBID, inLib, simScores, maxSim),
		)

		return si > sj
	})

	for i := range recordings {
		s := blendedScoreFull(
			float64(recordings[i].Score)/100.0,
			pop[recordings[i].MBID],
			maxPop,
			personalScore(recordings[i].MBID, inLib, simScores, maxSim),
		)
		recordings[i].Score = int(s * 100)
	}
}

// rerankReleaseGroups sorts release groups by blended score
// (text relevance + popularity + personalization) and updates their Score field.
func rerankReleaseGroups(rgs []MBReleaseGroup, pop map[string]int) {
	rerankReleaseGroupsPersonalized(rgs, pop, nil, nil)
}

func rerankReleaseGroupsPersonalized(
	rgs []MBReleaseGroup,
	pop map[string]int,
	inLib map[string]bool,
	simScores map[string]int,
) {
	if len(rgs) == 0 {
		return
	}

	maxPop := maxListenCount(pop)
	maxSim := maxSimScoreVal(simScores)

	sort.SliceStable(rgs, func(i, j int) bool {
		si := blendedScoreFull(
			float64(rgs[i].Score)/100.0,
			pop[rgs[i].MBID],
			maxPop,
			personalScore(rgs[i].MBID, inLib, simScores, maxSim),
		)
		sj := blendedScoreFull(
			float64(rgs[j].Score)/100.0,
			pop[rgs[j].MBID],
			maxPop,
			personalScore(rgs[j].MBID, inLib, simScores, maxSim),
		)

		return si > sj
	})

	for i := range rgs {
		s := blendedScoreFull(
			float64(rgs[i].Score)/100.0,
			pop[rgs[i].MBID],
			maxPop,
			personalScore(rgs[i].MBID, inLib, simScores, maxSim),
		)
		rgs[i].Score = int(s * 100)
	}
}

// maxSimScoreVal returns the highest similarity score in the map.
func maxSimScoreVal(scores map[string]int) int {
	maxVal := 0
	for _, v := range scores {
		if v > maxVal {
			maxVal = v
		}
	}

	return maxVal
}

// personalScore returns the personalization signal (0.0–1.0) for an MBID.
// Uses similarity scores from similar_artist_map, scaled by the max score
// in the batch so the most similar artist gets the full personalSimilar weight.
func personalScore(
	mbid string,
	inLib map[string]bool,
	simScores map[string]int,
	maxSimScore int,
) float64 {
	if inLib[mbid] {
		return personalInLibrary
	}

	if score, ok := simScores[mbid]; ok && score > 0 && maxSimScore > 0 {
		return personalSimilar * (float64(score) / float64(maxSimScore))
	}

	return 0.0
}

// ---------------------------------------------------------------------------
// Top Results — intent-scored cards
// ---------------------------------------------------------------------------

const (
	// topResultsMax is the maximum number of top-result cards to
	// return.  Bounded because they occupy expensive horizontal
	// screen real estate above the main search lists.
	topResultsMax = 5

	// topResultsPerCatMax caps how many cards from a single
	// category can appear in the final selection.  Keeps the row
	// from being all-artists or all-recordings on lopsided queries.
	topResultsPerCatMax = 2

	// topResultsMinScore is the absolute floor for a candidate's
	// final score (quality * prior).  Nothing below this survives,
	// regardless of category or rank.
	topResultsMinScore = 0.08

	// topResultsCandidates is how many candidates per category
	// feed into intent scoring.  Larger = more chances to surface
	// a better card, smaller = faster and less susceptible to
	// main-rerank noise.
	topResultsCandidates = 10

	// topResultsExactScanLimit caps how deep into each main result
	// list we'll scan for exact title/artist matches that didn't
	// make the top-N rerank.  This is the safety net for the case
	// where MB returns dozens of identically-relevant candidates
	// (covers of a popular song) and the rerank fails to surface
	// the canonical version because its popularity isn't indexed.
	topResultsExactScanLimit = 50

	// topResultsExactCap is how many exact-match candidates per
	// category can enter the candidate pool from the dedicated
	// ExactMatches retrieval source.
	topResultsExactCap = 3

	// topResultsClickDecay is the half-life of a per-query click
	// boost in days.  Longer = stickier, shorter = more
	// responsive to recent intent.
	topResultsClickDecay = 30.0

	// topResultsRowConfidence is the minimum gap between the
	// winning category's intent prior and the runner-up before we
	// show the row at all.  Below this we hide the row entirely
	// — it's better to show nothing than a wrong guess.
	topResultsRowConfidence = 0.12

	// Feature weights for candidate quality scoring.  Sum is not
	// required to be 1.0 because the final score is multiplied
	// by the intent prior separately.  Tune these against
	// specific query cases that behave wrong.
	fwExactTitle   = 1.00 // normalized title matches query exactly
	fwExactArtist  = 0.90 // artist name matches query exactly
	fwPrefixTitle  = 0.60 // title starts with query
	fwContainsWord = 0.40 // title contains query as a whole word
	fwContainsAny  = 0.20 // title has query as any substring
	fwListenLog    = 0.80 // log-scaled listen count (0 when 0 listens)
	fwListenerLog  = 0.60 // log-scaled listener count
	fwInLibrary    = 0.50 // owned by the user
	fwSimilar      = 0.20 // similar to an owned artist
	fwClusterBig   = 0.15 // release-group is a known canonical (many releases)
	fwOfficialOnly = 0.10 // official-status release only (not a bootleg)

	// priorAlpha controls how much the intent prior influences
	// final ranking.  Higher values make category dominance
	// more decisive; lower values let individual quality scores
	// win across categories.
	priorAlpha = 1.5
)

// resolveTopResults computes intent-scored top result cards from the
// already-reranked search results plus a dedicated exact-match
// retrieval source.  Returns 0-5 cards sorted by final score
// descending.
//
// Pipeline:
//  1. Retrieve candidates from three sources: top-N per category from
//     the main reranked result + exact title/artist matches from the
//     local index.  Union them, deduping by MBID.
//  2. Score each candidate using a featurized additive scorer with
//     explicit named features.  Quality is purely candidate-side; no
//     cross-candidate normalization.
//  3. Compute a category intent prior from the catalog signals
//     (listen-count distribution per category, query shape rules,
//     exact-match counts).  Multiply quality by prior^alpha.
//  4. Sort by final score, apply per-category caps and the row-level
//     confidence threshold.  Return up to topResultsMax cards.
func (e *Service) resolveTopResults(query string, result *MBSearchResult) []TopResult {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}

	// Stage 1: gather candidates.
	clicks := e.getSearchClicks(q)
	exactMatches := e.index.ExactMatches(q, topResultsExactCap)

	candidates := e.gatherTopCandidates(q, result, exactMatches, clicks)
	if len(candidates) == 0 {
		return nil
	}

	// Identify candidates that hit an exact-match feature so the
	// intent prior can boost their categories accordingly.  This
	// is what catches Blue October's "Calling You" — even if the
	// local index never heard of Blue October, the MB result list
	// has the recording with title == query, and the prior should
	// know that strengthens the recording category.  Composite
	// matches (query contains both the title and the artist of a
	// recording or album) are treated the same way.
	var exactCandidates []topCandidate

	for _, c := range candidates {
		isExact := isExactNameMatch(q, c.topResult.Name) ||
			isExactNameMatch(q, c.topResult.ArtistCredit) ||
			isCompositeMatch(q, c.topResult.Name, c.topResult.ArtistCredit)
		if isExact {
			exactCandidates = append(exactCandidates, c)
		}
	}

	// Stage 2: compute the category intent prior.
	prior := e.computeIntentPrior(q, result, exactMatches, exactCandidates)

	// Confidence gate: hide the row entirely if no category clearly
	// dominates.  Better to show nothing than a wrong guess.
	//
	// Override: if any candidate hits an exact match against an
	// entity with non-zero listener count, the row should always
	// show.  An exact match is itself a confidence signal — even
	// when shape and listener-distribution don't agree.
	confident := priorConfidence(prior) >= topResultsRowConfidence
	if !confident {
		for _, c := range exactCandidates {
			if c.qualityScore >= 1.0 { // exact match contributes >= fwExactTitle
				confident = true

				break
			}
		}
	}

	if !confident {
		e.logger.Info("search top results: prior too flat, hiding row",
			"query", query,
			"candidates", len(candidates),
			"exact_candidates", len(exactCandidates),
			"prior_artist", prior.artist,
			"prior_album", prior.album,
			"prior_recording", prior.recording,
		)

		return nil
	}

	// Stage 3: combine quality with prior.
	for i := range candidates {
		c := &candidates[i]

		var p float64

		switch c.category {
		case "artist":
			p = prior.artist
		case "release_group":
			p = prior.album
		case "recording":
			p = prior.recording
		}

		c.finalScore = c.qualityScore * math.Pow(p, priorAlpha)
	}

	// Stage 4: sort, dedupe by MBID, apply caps.
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].finalScore > candidates[j].finalScore
	})

	catCount := make(map[string]int, 3) //nolint:mnd
	seen := make(map[string]bool, len(candidates))

	var selected []TopResult

	for _, c := range candidates {
		if len(selected) >= topResultsMax {
			break
		}

		if c.finalScore < topResultsMinScore {
			break
		}

		if catCount[c.category] >= topResultsPerCatMax {
			continue
		}

		if seen[c.topResult.MBID] {
			continue
		}

		c.topResult.IntentScore = c.finalScore
		selected = append(selected, c.topResult)
		catCount[c.category]++
		seen[c.topResult.MBID] = true
	}

	if len(selected) > 0 {
		topName := selected[0].Name
		if selected[0].ArtistCredit != "" {
			topName = topName + " — " + selected[0].ArtistCredit
		}

		e.logger.Info("search top results selected",
			"query", query,
			"count", len(selected),
			"candidates", len(candidates),
			"exact_candidates", len(exactCandidates),
			"prior_artist", prior.artist,
			"prior_album", prior.album,
			"prior_recording", prior.recording,
			"top", topName,
			"top_score", selected[0].IntentScore,
		)
	}

	return selected
}

// topCandidate is a single scored candidate flowing through the
// top-results pipeline.  qualityScore is the per-candidate signal
// without category bias; finalScore is qualityScore multiplied by
// the category prior at selection time.
type topCandidate struct {
	topResult    TopResult
	category     string
	qualityScore float64
	finalScore   float64
}

// intentPrior is a probability distribution over the three entity
// categories: how likely the user is searching for an artist, an
// album, or a recording.  Sums to 1.0.
type intentPrior struct {
	artist    float64
	album     float64
	recording float64
}

// gatherTopCandidates builds the candidate pool from the top-N
// per category of the main reranked result plus exact-match results
// from two sources: the dedicated local-index ExactMatches lookup
// and any results in the MB list whose title/artist exactly equal
// the query.  Each candidate is scored once with the featurized
// quality scorer.  Duplicates (same MBID) are deduped, keeping the
// highest quality score.
func (e *Service) gatherTopCandidates(
	q string,
	result *MBSearchResult,
	exactMatches []SearchIndexResult,
	clicks map[string]searchClick,
) []topCandidate {
	candidates := make([]topCandidate, 0, topResultsCandidates*3+len(exactMatches))
	byMBID := make(map[string]int, cap(candidates))

	add := func(cand topCandidate) {
		if cand.topResult.MBID == "" {
			return
		}

		if existing, ok := byMBID[cand.topResult.MBID]; ok {
			if cand.qualityScore > candidates[existing].qualityScore {
				candidates[existing] = cand
			}

			return
		}

		byMBID[cand.topResult.MBID] = len(candidates)
		candidates = append(candidates, cand)
	}

	// Source 1: top-N artists from the main rerank.
	limit := topResultsCandidates
	if limit > len(result.Artists) {
		limit = len(result.Artists)
	}

	for i := range limit {
		a := result.Artists[i]

		quality := e.scoreArtistCandidate(q, &a, clicks)
		add(topCandidate{
			topResult: TopResult{
				EntityType: "artist",
				MBID:       a.MBID,
				Name:       a.Name,
				ArtistType: a.Type,
				Country:    a.Country,
				InLibrary:  a.InLibrary,
			},
			category:     "artist",
			qualityScore: quality,
		})
	}

	// Source 1b: scan the entire artist list (capped at
	// topResultsExactScanLimit) for exact name matches that didn't
	// make the top-N rerank.  Without this, an artist with a
	// perfect name match buried at position 12 by the rerank
	// could never become a top-result candidate.
	scanLimit := topResultsExactScanLimit
	if scanLimit > len(result.Artists) {
		scanLimit = len(result.Artists)
	}

	for i := topResultsCandidates; i < scanLimit; i++ {
		a := result.Artists[i]
		if !isExactNameMatch(q, a.Name) {
			continue
		}

		quality := e.scoreArtistCandidate(q, &a, clicks)
		add(topCandidate{
			topResult: TopResult{
				EntityType: "artist",
				MBID:       a.MBID,
				Name:       a.Name,
				ArtistType: a.Type,
				Country:    a.Country,
				InLibrary:  a.InLibrary,
			},
			category:     "artist",
			qualityScore: quality,
		})
	}

	// Source 2: top-N release groups from the main rerank.
	limit = topResultsCandidates
	if limit > len(result.ReleaseGroups) {
		limit = len(result.ReleaseGroups)
	}

	for i := range limit {
		rg := result.ReleaseGroups[i]

		quality := e.scoreReleaseGroupCandidate(q, &rg, clicks)

		year := ""
		if len(rg.FirstReleaseDate) >= 4 { //nolint:mnd
			year = rg.FirstReleaseDate[:4]
		}

		add(topCandidate{
			topResult: TopResult{
				EntityType:   "release_group",
				MBID:         rg.MBID,
				Name:         rg.Title,
				ArtistCredit: rg.ArtistCredit,
				PrimaryType:  rg.PrimaryType,
				Year:         year,
				InLibrary:    rg.InLibrary,
			},
			category:     "release_group",
			qualityScore: quality,
		})
	}

	// Source 2b: scan the rest of the release-group list for
	// exact title or artist matches.  Same rationale as Source 1b.
	// Also catches composite matches (e.g. "abbey road beatles").
	scanLimit = topResultsExactScanLimit
	if scanLimit > len(result.ReleaseGroups) {
		scanLimit = len(result.ReleaseGroups)
	}

	for i := topResultsCandidates; i < scanLimit; i++ {
		rg := result.ReleaseGroups[i]

		exactTitle := isExactNameMatch(q, rg.Title)
		exactArtist := isExactNameMatch(q, rg.ArtistCredit)
		composite := isCompositeMatch(q, rg.Title, rg.ArtistCredit)

		if !exactTitle && !exactArtist && !composite {
			continue
		}

		quality := e.scoreReleaseGroupCandidate(q, &rg, clicks)
		if composite && !exactTitle && !exactArtist {
			quality += fwExactTitle
		}

		year := ""
		if len(rg.FirstReleaseDate) >= 4 { //nolint:mnd
			year = rg.FirstReleaseDate[:4]
		}

		add(topCandidate{
			topResult: TopResult{
				EntityType:   "release_group",
				MBID:         rg.MBID,
				Name:         rg.Title,
				ArtistCredit: rg.ArtistCredit,
				PrimaryType:  rg.PrimaryType,
				Year:         year,
				InLibrary:    rg.InLibrary,
			},
			category:     "release_group",
			qualityScore: quality,
		})
	}

	// Source 3: top-N recordings from the main rerank.
	limit = topResultsCandidates
	if limit > len(result.Recordings) {
		limit = len(result.Recordings)
	}

	for i := range limit {
		r := result.Recordings[i]

		quality := e.scoreRecordingCandidate(q, &r, clicks)
		add(topCandidate{
			topResult: TopResult{
				EntityType:   "recording",
				MBID:         r.MBID,
				Name:         r.Title,
				ArtistCredit: r.ArtistCredit,
				Length:       r.Length,
				InLibrary:    r.InLibrary,
			},
			category:     "recording",
			qualityScore: quality,
		})
	}

	// Source 3b: scan the rest of the recording list for exact
	// matches.  This is the critical fix for the case where MB
	// returns 75 recordings all with relevance 100 — the rerank
	// can only differentiate them by popularity (which may be
	// missing for many), so a popular exact match like Blue
	// October's "Calling You" might land at position 11+.  By
	// scanning the full list for exact matches, we surface them
	// regardless of where the rerank put them.
	//
	// Also catches "composite" matches: when the query contains
	// both the recording title AND the artist credit (e.g.
	// "calling you blue october"), the recording is a strong
	// candidate even though neither field equals the full query.
	scanLimit = topResultsExactScanLimit
	if scanLimit > len(result.Recordings) {
		scanLimit = len(result.Recordings)
	}

	for i := topResultsCandidates; i < scanLimit; i++ {
		r := result.Recordings[i]

		exactTitle := isExactNameMatch(q, r.Title)
		exactArtist := isExactNameMatch(q, r.ArtistCredit)
		composite := isCompositeMatch(q, r.Title, r.ArtistCredit)

		if !exactTitle && !exactArtist && !composite {
			continue
		}

		quality := e.scoreRecordingCandidate(q, &r, clicks)

		// Composite matches don't get the exact-title feature
		// from the scorer (because neither field equals the
		// query), so add the bonus explicitly here so they
		// compete with title-only exact matches.
		if composite && !exactTitle && !exactArtist {
			quality += fwExactTitle
		}

		add(topCandidate{
			topResult: TopResult{
				EntityType:   "recording",
				MBID:         r.MBID,
				Name:         r.Title,
				ArtistCredit: r.ArtistCredit,
				Length:       r.Length,
				InLibrary:    r.InLibrary,
			},
			category:     "recording",
			qualityScore: quality,
		})
	}

	// Source 4: exact matches from the local index.  These bypass
	// the main rerank entirely so a high-popularity entity buried
	// at position 8 in the MB result list still gets surfaced.
	for _, m := range exactMatches {
		quality := e.scoreExactMatch(q, &m, clicks)

		switch m.EntityType {
		case "artist":
			add(topCandidate{
				topResult: TopResult{
					EntityType: "artist",
					MBID:       m.MBID,
					Name:       m.Title,
					ArtistType: m.ArtistType,
					Country:    m.Country,
					InLibrary:  m.InLibrary || m.LocalArtistID > 0,
				},
				category:     "artist",
				qualityScore: quality,
			})
		case "release_group":
			year := ""
			if len(m.ReleaseDate) >= 4 { //nolint:mnd
				year = m.ReleaseDate[:4]
			}

			add(topCandidate{
				topResult: TopResult{
					EntityType:   "release_group",
					MBID:         m.MBID,
					Name:         m.Title,
					ArtistCredit: m.ArtistName,
					PrimaryType:  m.PrimaryType,
					Year:         year,
					InLibrary:    m.InLibrary || m.LocalReleaseGroupID > 0,
				},
				category:     "release_group",
				qualityScore: quality,
			})
		case "recording":
			add(topCandidate{
				topResult: TopResult{
					EntityType:   "recording",
					MBID:         m.MBID,
					Name:         m.Title,
					ArtistCredit: m.ArtistName,
					Length:       m.Duration,
					InLibrary:    m.InLibrary || m.LocalRecordingID > 0,
				},
				category:     "recording",
				qualityScore: quality,
			})
		}
	}

	return candidates
}

// scoreArtistCandidate computes the featurized quality score for an
// artist top-result candidate.  Pure additive — no cross-candidate
// normalization, no popularity squaring.
func (e *Service) scoreArtistCandidate(
	q string,
	a *MBArtist,
	clicks map[string]searchClick,
) float64 {
	name := strings.ToLower(a.Name)
	qn := normalizeForMatch(q)
	nn := normalizeForMatch(a.Name)

	score := 0.0

	switch {
	case nn == qn:
		score += fwExactTitle
	case strings.HasPrefix(name, q):
		score += fwPrefixTitle
	case containsWord(name, q):
		score += fwContainsWord
	case strings.Contains(name, q):
		score += fwContainsAny
	}

	score += fwListenLog * normLog(a.Popularity)
	score += fwListenerLog * normLog(a.ListenerCount)

	if a.InLibrary {
		score += fwInLibrary
	}

	if cb := clicks[a.MBID]; cb.count > 0 {
		score += clickFeature(cb)
	}

	return score
}

// scoreReleaseGroupCandidate computes the featurized quality score
// for a release-group top-result candidate.
func (e *Service) scoreReleaseGroupCandidate(
	q string,
	rg *MBReleaseGroup,
	clicks map[string]searchClick,
) float64 {
	title := strings.ToLower(rg.Title)
	credit := strings.ToLower(rg.ArtistCredit)
	qn := normalizeForMatch(q)
	tn := normalizeForMatch(rg.Title)
	cn := normalizeForMatch(rg.ArtistCredit)

	score := 0.0

	switch {
	case tn == qn:
		score += fwExactTitle
	case cn == qn && len(qn) >= 3: //nolint:mnd
		score += fwExactArtist
	case strings.HasPrefix(title, q):
		score += fwPrefixTitle
	case containsWord(title, q):
		score += fwContainsWord
	case strings.Contains(title, q):
		score += fwContainsAny
	}

	score += fwListenLog * normLog(rg.Popularity)
	score += fwListenerLog * normLog(rg.ListenerCount)

	if rg.InLibrary {
		score += fwInLibrary
	}

	// Penalize "Various Artists" compilations — they tend to dominate
	// covers searches without being what the user wants.
	if strings.Contains(credit, "various artists") {
		score *= 0.5 //nolint:mnd
	}

	if cb := clicks[rg.MBID]; cb.count > 0 {
		score += clickFeature(cb)
	}

	return score
}

// scoreRecordingCandidate computes the featurized quality score for
// a recording top-result candidate.
func (e *Service) scoreRecordingCandidate(
	q string,
	r *MBRecording,
	clicks map[string]searchClick,
) float64 {
	title := strings.ToLower(r.Title)
	qn := normalizeForMatch(q)
	tn := normalizeForMatch(r.Title)
	cn := normalizeForMatch(r.ArtistCredit)

	score := 0.0

	switch {
	case tn == qn:
		score += fwExactTitle
	case cn == qn && len(qn) >= 3: //nolint:mnd
		score += fwExactArtist
	case strings.HasPrefix(title, q):
		score += fwPrefixTitle
	case containsWord(title, q):
		score += fwContainsWord
	case strings.Contains(title, q):
		score += fwContainsAny
	}

	score += fwListenLog * normLog(r.Popularity)
	score += fwListenerLog * normLog(r.ListenerCount)

	if r.InLibrary {
		score += fwInLibrary
	}

	if cb := clicks[r.MBID]; cb.count > 0 {
		score += clickFeature(cb)
	}

	return score
}

// scoreExactMatch computes a featurized quality score for a
// candidate sourced from ExactMatches.  Always assigns the exact
// match feature bonus on top of the standard quality features so
// that exact matches reliably outrank fuzzy ones.
func (e *Service) scoreExactMatch(
	q string,
	m *SearchIndexResult,
	clicks map[string]searchClick,
) float64 {
	title := strings.ToLower(m.Title)
	credit := strings.ToLower(m.ArtistName)

	score := 0.0

	switch {
	case title == q:
		score += fwExactTitle
	case credit == q:
		score += fwExactArtist
	default:
		// Shouldn't happen — ExactMatches only returns rows whose
		// title or artist matches.  Defensive fallback.
		score += fwContainsWord
	}

	score += fwListenLog * normLog(m.Popularity)
	score += fwListenerLog * normLog(m.ListenerCount)

	if m.InLibrary || m.LocalArtistID > 0 || m.LocalReleaseGroupID > 0 || m.LocalRecordingID > 0 {
		score += fwInLibrary
	}

	if cb := clicks[m.MBID]; cb.count > 0 {
		score += clickFeature(cb)
	}

	return score
}

// computeIntentPrior derives a category probability distribution
// from the query shape and the catalog signals available in the
// candidate pool.  Returns weights summing to ~1.0.
//
// Strategy: start with a uniform prior, then apply signal-based
// adjustments.  The strongest signals (exact name match against a
// popular artist, dominant track-cover-wave pattern) bias the prior
// hard; weaker signals (query length, listen-count distribution)
// nudge it.  Finally normalize to a probability distribution.
//
// The exactCandidates parameter is the list of candidates that hit
// an exact-match feature (either via the local index ExactMatches
// retrieval or via the MB result-list scan in gatherTopCandidates).
// These provide the strongest evidence we have for "the user means
// this category" and dominate weaker signals.
func (e *Service) computeIntentPrior(
	q string,
	result *MBSearchResult,
	exactMatches []SearchIndexResult,
	exactCandidates []topCandidate,
) intentPrior {
	// Start with a slight lean toward recordings — most music
	// searches in practice are for songs.  Mild enough that
	// other signals can override.
	weights := intentPrior{
		artist:    1.0,
		album:     1.0,
		recording: 1.2, //nolint:mnd
	}

	// Signal: query length (word count).  Single-word queries skew
	// strongly artist; long queries skew strongly toward
	// titles (album or recording).
	wordCount := len(strings.Fields(q))

	switch {
	case wordCount == 1:
		weights.artist *= 2.0    //nolint:mnd
		weights.album *= 0.7     //nolint:mnd
		weights.recording *= 0.7 //nolint:mnd
	case wordCount >= 4: //nolint:mnd
		weights.artist *= 0.5    //nolint:mnd
		weights.album *= 1.2     //nolint:mnd
		weights.recording *= 1.3 //nolint:mnd
	}

	// Signal: exact matches in the local index.  An exact match
	// against a popular artist is the strongest evidence we
	// have for "the user means this artist".  Scale by listener
	// count so a popular exact match dominates and an obscure
	// one doesn't move the needle.
	for _, m := range exactMatches {
		if !isExactNameMatch(q, m.Title) && !isExactNameMatch(q, m.ArtistName) {
			continue
		}

		// Confidence boost scales with log listener count.
		boost := 1.0 + 1.5*normLog(m.ListenerCount) //nolint:mnd

		switch m.EntityType {
		case "artist":
			weights.artist *= boost
		case "release_group":
			weights.album *= boost
		case "recording":
			weights.recording *= boost
		}
	}

	// Signal: exact-match candidates discovered in the MB result
	// list (Source 1b/2b/3b in gatherTopCandidates).  These cover
	// the case where the local index doesn't have the entity but
	// MB does — e.g. Blue October's "Calling You" when Blue
	// October isn't yet a known artist.  Same scaling as
	// index-sourced exact matches.
	for _, c := range exactCandidates {
		var listeners int

		switch c.category {
		case "artist":
			listeners = artistListenerByMBID(result.Artists, c.topResult.MBID)
		case "release_group":
			listeners = rgListenerByMBID(result.ReleaseGroups, c.topResult.MBID)
		case "recording":
			listeners = recListenerByMBID(result.Recordings, c.topResult.MBID)
		}

		boost := 1.0 + 1.0*normLog(listeners) //nolint:mnd

		switch c.category {
		case "artist":
			weights.artist *= boost
		case "release_group":
			weights.album *= boost
		case "recording":
			weights.recording *= boost
		}
	}

	// Signal: many recordings in the result list with the same
	// title as the query → cover-wave pattern → strong recording.
	titleMatches := 0

	for _, r := range result.Recordings {
		if isExactNameMatch(q, r.Title) {
			titleMatches++
		}
	}

	if titleMatches >= 5 { //nolint:mnd
		weights.recording *= 1.8 //nolint:mnd
	} else if titleMatches >= 2 { //nolint:mnd
		weights.recording *= 1.3 //nolint:mnd
	}

	// Signal: aggregate listener count per category in the
	// candidate pool.  Sum the top 5 per category and use the
	// proportional split as a soft nudge.  Recordings naturally
	// have higher listen counts than albums (each play increments
	// the recording, not the album), so we use *listener* count
	// rather than *listen* count to dampen that bias.
	artistListeners := sumTopListeners(artistListenerCounts(result.Artists), 5)  //nolint:mnd
	albumListeners := sumTopListeners(rgListenerCounts(result.ReleaseGroups), 5) //nolint:mnd
	recListeners := sumTopListeners(recListenerCounts(result.Recordings), 5)     //nolint:mnd

	totalListeners := artistListeners + albumListeners + recListeners
	if totalListeners > 0 {
		// Apply as a 0.5x nudge so it doesn't override stronger
		// signals.  We'd rather trust shape and exact matches
		// than raw listener distributions.
		weights.artist *= 1.0 + 0.5*float64(artistListeners)/float64(totalListeners) //nolint:mnd
		weights.album *= 1.0 + 0.5*float64(albumListeners)/float64(totalListeners)   //nolint:mnd
		weights.recording *= 1.0 + 0.5*float64(recListeners)/float64(totalListeners) //nolint:mnd
	}

	// Normalize to a probability distribution.
	total := weights.artist + weights.album + weights.recording
	if total <= 0 {
		return intentPrior{artist: 1.0 / 3.0, album: 1.0 / 3.0, recording: 1.0 / 3.0} //nolint:mnd
	}

	return intentPrior{
		artist:    weights.artist / total,
		album:     weights.album / total,
		recording: weights.recording / total,
	}
}

// artistListenerByMBID returns the listener count for the artist
// with the given MBID, or 0 when not found.
func artistListenerByMBID(arts []MBArtist, mbid string) int {
	for _, a := range arts {
		if a.MBID == mbid {
			return a.ListenerCount
		}
	}

	return 0
}

func rgListenerByMBID(rgs []MBReleaseGroup, mbid string) int {
	for _, rg := range rgs {
		if rg.MBID == mbid {
			return rg.ListenerCount
		}
	}

	return 0
}

func recListenerByMBID(recs []MBRecording, mbid string) int {
	for _, r := range recs {
		if r.MBID == mbid {
			return r.ListenerCount
		}
	}

	return 0
}

// priorConfidence returns the difference between the largest and
// second-largest values in the prior, as a quick proxy for "how
// sure is the prior about its top pick".  Range is 0 (totally flat,
// i.e. uniform 1/3) to 1 (one category at 1.0, others at 0).
func priorConfidence(p intentPrior) float64 {
	vals := [3]float64{p.artist, p.album, p.recording}

	maxVal := vals[0]
	for _, v := range vals[1:] {
		if v > maxVal {
			maxVal = v
		}
	}

	secondMax := 0.0
	for _, v := range vals {
		if v < maxVal && v > secondMax {
			secondMax = v
		}
	}

	return maxVal - secondMax
}

// normLog returns log10(n+1) / log10(maxScale+1), clamped to [0, 1].
// maxScale is a fixed reference point so the function is stable
// across queries — different from popRank which normalizes to a
// dynamic per-query max.
func normLog(n int) float64 {
	if n <= 0 {
		return 0
	}

	const maxScale = 50_000_000 // top-tier artists have ~10-150M listens

	v := math.Log10(float64(n)+1) / math.Log10(maxScale+1) //nolint:mnd
	if v > 1.0 {
		return 1.0
	}

	return v
}

// clickFeature returns the additive feature contribution from a
// per-query click record.  Bounded so a click streak can't
// dominate the rest of the score.
func clickFeature(c searchClick) float64 {
	daysSince := time.Since(c.lastClicked).Hours() / 24.0 //nolint:mnd
	recency := 1.0 / (1.0 + daysSince/topResultsClickDecay)
	boost := math.Log2(float64(c.count)+1) * recency * 0.3 //nolint:mnd

	if boost > 0.6 { //nolint:mnd
		return 0.6
	}

	return boost
}

// artistListenerCounts and friends extract the per-entity listener
// count slice for the listener-distribution prior signal.
func artistListenerCounts(arts []MBArtist) []int {
	out := make([]int, len(arts))
	for i, a := range arts {
		out[i] = a.ListenerCount
	}

	return out
}

func rgListenerCounts(rgs []MBReleaseGroup) []int {
	out := make([]int, len(rgs))
	for i, rg := range rgs {
		out[i] = rg.ListenerCount
	}

	return out
}

func recListenerCounts(recs []MBRecording) []int {
	out := make([]int, len(recs))
	for i, r := range recs {
		out[i] = r.ListenerCount
	}

	return out
}

// sumTopListeners returns the sum of the top n entries in xs.
// Used by the listener-distribution prior signal.
func sumTopListeners(xs []int, n int) int {
	if len(xs) == 0 {
		return 0
	}

	sorted := make([]int, len(xs))
	copy(sorted, xs)

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] > sorted[j]
	})

	if n > len(sorted) {
		n = len(sorted)
	}

	sum := 0
	for i := range n {
		sum += sorted[i]
	}

	return sum
}

// containsWord checks if text contains word as a whole word bounded
// by spaces, hyphens, or string boundaries.
func containsWord(text, word string) bool {
	idx := strings.Index(text, word)
	if idx < 0 {
		return false
	}

	// Check left boundary.
	if idx > 0 {
		c := text[idx-1]
		if c != ' ' && c != '-' && c != '(' && c != '[' {
			return false
		}
	}

	// Check right boundary.
	end := idx + len(word)
	if end < len(text) {
		c := text[end]
		if c != ' ' && c != '-' && c != ')' && c != ']' {
			return false
		}
	}

	return true
}

// isExactNameMatch returns true when the (already lowercased) query
// is equal to the (raw-cased) name after lowercasing and trimming.
// Punctuation is normalized so "Party in the U.S.A." matches
// "party in the usa".  Used by the top-results pipeline to find
// exact matches anywhere in the main result lists, not just in the
// top-N positions the main rerank produced.
func isExactNameMatch(q, name string) bool {
	if name == "" {
		return false
	}

	return normalizeForMatch(name) == normalizeForMatch(q)
}

// isCompositeMatch returns true when the query contains both `title`
// and `artist` as normalized substrings — e.g. "calling you blue
// october" composes "calling you" + "blue october" so the user
// probably wants Blue October's "Calling You".  Both fragments must
// be at least 3 characters to be considered.
//
// This is the heuristic version of entity linking: instead of
// training a model to identify "title + artist" multi-entity
// queries, we just notice when a candidate's title and artist both
// appear inside the user's query.
func isCompositeMatch(q, title, artist string) bool {
	if len(title) < 3 || len(artist) < 3 { //nolint:mnd
		return false
	}

	qn := normalizeForMatch(q)
	tn := normalizeForMatch(title)
	an := normalizeForMatch(artist)

	if tn == "" || an == "" || qn == "" {
		return false
	}

	// Both fragments must appear in the query.  Order doesn't
	// matter — "calling you blue october" and "blue october
	// calling you" should both match.
	return strings.Contains(qn, tn) && strings.Contains(qn, an)
}

// normalizeForMatch lowercases, trims, and strips ASCII punctuation
// other than internal whitespace so titles like "Party in the U.S.A.",
// "Party In the U.S.A", and "party in the usa" all collapse to the
// same normalized form.  Cheap O(n) — no regex.
func normalizeForMatch(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))

	var b strings.Builder
	b.Grow(len(s))

	prevSpace := false

	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= '0' && r <= '9',
			r >= 0x80: // keep non-ASCII as-is
			b.WriteRune(r)

			prevSpace = false
		case r == ' ' || r == '\t':
			if !prevSpace && b.Len() > 0 {
				b.WriteByte(' ')

				prevSpace = true
			}
		default:
			// Drop punctuation entirely (not even replaced with
			// a space).  This collapses "U.S.A." to "usa" so it
			// matches the dot-less form.
		}
	}

	out := b.String()
	if prevSpace && len(out) > 0 {
		out = out[:len(out)-1]
	}

	return out
}

type searchClick struct {
	count       int
	lastClicked time.Time
}

// getSearchClicks returns click history for a query.
func (e *Service) getSearchClicks(query string) map[string]searchClick {
	rows, err := e.db.QueryContext(
		"SELECT entity_mbid, click_count, last_clicked FROM search_clicks WHERE query = ?",
		query,
	)
	if err != nil {
		return nil
	}

	defer func() { _ = rows.Close() }()

	result := make(map[string]searchClick)

	for rows.Next() {
		var (
			mbid        string
			count       int
			lastClicked time.Time
		)

		if err := rows.Scan(&mbid, &count, &lastClicked); err == nil {
			result[mbid] = searchClick{count: count, lastClicked: lastClicked}
		}
	}

	return result
}

// RecordSearchClick records that the user clicked a search result.
// Called from the frontend when any search result is clicked.
func (e *Service) RecordSearchClick(query, mbid, entityType string) {
	if query == "" || mbid == "" {
		return
	}

	q := strings.ToLower(strings.TrimSpace(query))

	_, _ = e.db.ExecContext(`
		INSERT INTO search_clicks (query, entity_mbid, entity_type, click_count, last_clicked)
		VALUES (?, ?, ?, 1, CURRENT_TIMESTAMP)
		ON CONFLICT(query, entity_mbid) DO UPDATE SET
			click_count = click_count + 1,
			last_clicked = CURRENT_TIMESTAMP
	`, q, mbid, entityType)
}

// blendedScore computes relevanceWeight*relevance + popularityWeight*logPop.
// relevance is 0–1. listenCount is raw; maxListenCount is the
// maximum in the result set (for normalization).
//
//nolint:unused // referenced by deferred MB search rework.
func blendedScore(relevance float64, listenCount, maxListenCount int) float64 {
	return blendedScoreFull(relevance, listenCount, maxListenCount, 0.0)
}

// blendedScoreFull computes the weighted blend of relevance, popularity,
// and personalization.  personalization is 0.0–1.0.
func blendedScoreFull(
	relevance float64,
	listenCount, maxListenCount int,
	personalization float64,
) float64 {
	effectiveMax := maxListenCount
	if effectiveMax < 100_000 { //nolint:mnd
		effectiveMax = 100_000
	}

	logPop := math.Log10(float64(listenCount)+1) / math.Log10(float64(effectiveMax)+1)

	return relevanceWeight*relevance + popularityWeight*logPop + personalizationWeight*personalization
}

// dynamicSearchLimit computes the number of results to request from
// MB based on the total match count.  Returns at least mbSearchLimit
// and at most mbSearchMaxLimit.  Aims for ~15% of total matches so
// the ranking pipeline has enough candidates to surface popular
// results that MB's text relevance alone would bury.
//
//nolint:unused // referenced by deferred MB search rework.
func dynamicSearchLimit(totalMatches int) int {
	if totalMatches <= mbSearchLimit {
		return mbSearchLimit
	}

	// 15% of total matches, but floor to mbSearchLimit and
	// cap to mbSearchMaxLimit (and MB's API max of 100).
	want := totalMatches * 15 / 100 //nolint:mnd
	if want < mbSearchLimit {
		want = mbSearchLimit
	}

	if want > mbSearchMaxLimit {
		want = mbSearchMaxLimit
	}

	return want
}

// maxListenCount returns the highest listen count in the map.
func maxListenCount(pop map[string]int) int {
	maxVal := 0

	for _, v := range pop {
		if v > maxVal {
			maxVal = v
		}
	}

	return maxVal
}

// listenCounts extracts a simple mbid→listenCount map from PopularityData.
func listenCounts(pop map[string]PopularityData) map[string]int {
	out := make(map[string]int, len(pop))
	for mbid, d := range pop {
		out[mbid] = d.ListenCount
	}

	return out
}

// ---------------------------------------------------------------------------
// Lucene query building
// ---------------------------------------------------------------------------

// luceneSpecialChars are characters that have special meaning in
// Lucene query syntax and must be escaped in user input.
var luceneSpecialChars = strings.NewReplacer( //nolint:gochecknoglobals
	`\`, `\\`,
	`+`, `\+`,
	`-`, `\-`,
	`!`, `\!`,
	`(`, `\(`,
	`)`, `\)`,
	`{`, `\{`,
	`}`, `\}`,
	`[`, `\[`,
	`]`, `\]`,
	`^`, `\^`,
	`"`, `\"`,
	`~`, `\~`,
	`*`, `\*`,
	`?`, `\?`,
	`:`, `\:`,
	`/`, `\/`,
)

// buildLuceneQuery converts a user's search input into a Lucene
// AND query with a wildcard on the last term for type-ahead.
//
// Examples:
//
//	"radiohead"       → "radiohead*"
//	"the teenagers"   → "the AND teenagers*"
//	"florence machine" → "florence AND machine*"
//	"ac/dc"           → "ac\/dc*"
//
// This eliminates the common-word pollution problem: "the teenagers"
// no longer matches "The Beatles" (which only contains "the").
// The trailing wildcard enables prefix matching as the user types.
func buildLuceneQuery(input string) string {
	words := strings.Fields(strings.TrimSpace(input))
	if len(words) == 0 {
		return ""
	}

	// Escape special Lucene characters in each word.
	for i, w := range words {
		words[i] = luceneSpecialChars.Replace(w)
	}

	if len(words) == 1 {
		return words[0] + "*"
	}

	// AND all terms, wildcard on the last (type-ahead).
	var b strings.Builder

	for i, w := range words {
		if i > 0 {
			b.WriteString(" AND ")
		}

		b.WriteString(w)

		if i == len(words)-1 {
			b.WriteByte('*')
		}
	}

	return b.String()
}

// buildLuceneQueryWithArtist builds a Lucene query that searches
// both the entity's own field (title) and the artist credit field.
// For "queen": (releasegroup:queen* OR artist:queen*)
// This ensures searches return results BY the artist, not just
// results with the query in the title.
func buildLuceneQueryWithArtist(input, entityField, artistField string) string {
	words := strings.Fields(strings.TrimSpace(input))
	if len(words) == 0 {
		return ""
	}

	for i, w := range words {
		words[i] = luceneSpecialChars.Replace(w)
	}

	// Build the base query terms.
	base := buildLuceneQuery(input)

	// Single word: (field:word* OR artist:word*)
	if len(words) == 1 {
		return "(" + entityField + ":" + base + " OR " + artistField + ":" + base + ")"
	}

	// Multi-word: (field:(term1 AND term2*) OR artist:(term1 AND term2*))
	return "(" + entityField + ":(" + base + ") OR " + artistField + ":(" + base + "))"
}
