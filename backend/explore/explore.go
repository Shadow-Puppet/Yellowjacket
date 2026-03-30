package explore

import (
	"context"
	"encoding/json"
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
	mb        *MusicBrainzClient
	lb        *ListenBrainzClient
	cache     *Cache
	index     *SearchIndex
	artProxy  *CoverArtProxy
	artistImg *ArtistImageProvider
	libMBID   *LibraryMBIDIndex
	logger    *slog.Logger
	ctx       context.Context
}

// NewExploreService creates a Service backed by the given
// database.  It instantiates the rate limiter, cache, MusicBrainz
// client, and ListenBrainz client internally.
func NewExploreService(logger *slog.Logger, db *database.DB) *Service {
	cache := NewCache(db, logger.WithGroup("cache"))
	lbLimiter := NewRateLimiter()
	// MB search limiter: burst of 3 (covers one search's 3 concurrent calls)
	// then 1/sec refill.  The musicbrainzws2 library retries on 429 as backup.
	mbSearchLimiter := NewRateLimiterBurst(1, 3)
	// MB background limiter: strict 1/sec for sustained image resolution calls.
	mbBackgroundLimiter := NewRateLimiter()
	mb := NewMusicBrainzClient(cache, mbSearchLimiter, logger.WithGroup("musicbrainz"))
	lb := NewListenBrainzClient(lbLimiter, cache, logger.WithGroup("listenbrainz"))
	artProxy := NewCoverArtProxy(db, lbLimiter)
	artistImg := NewArtistImageProvider(
		db, cache, mbBackgroundLimiter, logger.WithGroup("artist-image"),
	)
	index := NewSearchIndex(db, lb, artistImg, logger.WithGroup("search-index"))
	index.MarkReadyIfPopulated() // make index queryable immediately if data exists
	libMBID := NewLibraryMBIDIndex(db)

	logger.Info("explore service created")

	return &Service{
		mb:        mb,
		lb:        lb,
		cache:     cache,
		index:     index,
		artProxy:  artProxy,
		artistImg: artistImg,
		libMBID:   libMBID,
		logger:    logger,
		ctx:       context.Background(),
	}
}

// SetContext injects the Wails runtime context.  Called from
// OnStartup after the Wails runtime is initialised.
func (e *Service) SetContext(ctx context.Context) {
	e.ctx = ctx
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
	return e.mb.SearchArtists(e.ctx, query, mbSearchLimit)
}

// SearchReleaseGroups queries MusicBrainz for release groups matching the query.
func (e *Service) SearchReleaseGroups(query string) ([]MBReleaseGroup, error) {
	return e.mb.SearchReleaseGroups(e.ctx, query, mbSearchLimit)
}

// SearchRecordings queries MusicBrainz for recordings matching the query.
func (e *Service) SearchRecordings(query string) ([]MBRecording, error) {
	return e.mb.SearchRecordings(e.ctx, query, mbSearchLimit)
}

// SearchLocal queries only the local FTS5 index and returns results
// instantly with no network calls.  Returns nil if the index isn't
// ready.  The frontend calls this in parallel with Search() to show
// instant results while the full pipeline runs.
func (e *Service) SearchLocal(query string) *MBSearchResult {
	indexHits := e.index.Search(query, 30) //nolint:mnd
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
func (e *Service) LookupArtist(mbid string) (*MBArtist, error) {
	return e.mb.LookupArtist(e.ctx, mbid)
}

// LookupReleaseGroup fetches a single MusicBrainz release group by MBID.
func (e *Service) LookupReleaseGroup(mbid string) (*MBReleaseGroup, error) {
	return e.mb.LookupReleaseGroup(e.ctx, mbid)
}

// ---------------------------------------------------------------------------
// MusicBrainz browse
// ---------------------------------------------------------------------------

// BrowseReleaseGroups fetches release groups for a given artist MBID.
// Also adds results to the search index (Tier 5: organic growth).
func (e *Service) BrowseReleaseGroups(artistMBID string) ([]MBReleaseGroup, error) {
	rgs, err := e.mb.BrowseReleaseGroups(e.ctx, artistMBID)
	if err != nil {
		return nil, err
	}

	// Tier 5: organic growth — index this discography.
	// Look up the artist name from the first result's credit, or
	// fall back to the MBID.
	artistName := artistMBID

	artist, lookupErr := e.mb.LookupArtist(e.ctx, artistMBID)
	if lookupErr == nil && artist != nil {
		artistName = artist.Name
	}

	go e.index.AddFromCache(artistName, artistMBID, rgs)

	return rgs, nil
}

// BrowseReleases fetches releases for a given release group MBID.
func (e *Service) BrowseReleases(releaseGroupMBID string) ([]MBRelease, error) {
	return e.mb.BrowseReleases(e.ctx, releaseGroupMBID)
}

// ---------------------------------------------------------------------------
// ListenBrainz
// ---------------------------------------------------------------------------

// TopRecordingsForArtist returns the most-listened recordings for an artist.
func (e *Service) TopRecordingsForArtist(artistMBID string) ([]LBTopRecording, error) {
	return e.lb.TopRecordingsForArtist(e.ctx, artistMBID)
}

// TopReleaseGroupsForArtist returns the most-listened release groups for an artist.
func (e *Service) TopReleaseGroupsForArtist(artistMBID string) ([]LBTopReleaseGroup, error) {
	return e.lb.TopReleaseGroupsForArtist(e.ctx, artistMBID)
}

// SimilarArtists returns artists similar to the given artist MBID.
func (e *Service) SimilarArtists(artistMBID string) ([]LBSimilarArtist, error) {
	return e.lb.SimilarArtists(e.ctx, artistMBID)
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

// ThumbnailRequest is a single item in a batch thumbnail request.
type ThumbnailRequest struct {
	MBID       string `json:"mbid"`
	AlbumName  string `json:"albumName"`
	ArtistName string `json:"artistName"`
}

// GetThumbnails fetches multiple thumbnails in one call and returns
// a map of MBID → base64 data URL.  Entries with no art are omitted.
func (e *Service) GetThumbnails(requests []ThumbnailRequest) map[string]string {
	result := make(map[string]string, len(requests))

	for _, req := range requests {
		dataURL := e.artProxy.GetThumbnail(req.MBID, req.AlbumName, req.ArtistName)
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

// CheckLibraryMBIDs returns which of the given MBIDs exist in the
// local music library.  Returns a map of MBID → entity type
// ("artist", "release_group", "recording").
func (e *Service) CheckLibraryMBIDs(mbids []string) map[string]string {
	return e.libMBID.CheckMBIDs(mbids)
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
	e.logger.Info("search started", "query", query)

	// Phase 0: query local popularity index (instant, no API calls).
	p0Start := time.Now()
	indexHits := e.index.Search(query, 30) //nolint:mnd
	p0Dur := time.Since(p0Start)

	e.logger.Info("search phase 0 complete (index)",
		"query", query,
		"hits", len(indexHits),
		"elapsed", p0Dur,
	)

	// Phase 1: concurrent MB search (3 goroutines) with a deadline
	// so a slow MusicBrainz server doesn't hold up the whole search.
	p1Start := time.Now()

	mbCtx, mbCancel := context.WithTimeout(e.ctx, searchMBTimeout)
	defer mbCancel()

	var (
		result MBSearchResult
		mu     sync.Mutex
		wg     sync.WaitGroup
	)

	type searchFunc struct {
		name string
		fn   func()
	}

	searches := []searchFunc{
		{
			name: "artists",
			fn: func() {
				t := time.Now()
				artists, err := e.mb.SearchArtists(mbCtx, query, mbSearchLimit)

				e.logger.Info("search MB sub-call",
					"entity", "artists",
					"elapsed", time.Since(t).Round(time.Millisecond),
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
				result.Artists = artists
				mu.Unlock()
			},
		},
		{
			name: "releaseGroups",
			fn: func() {
				t := time.Now()
				rgs, err := e.mb.SearchReleaseGroups(mbCtx, query, mbSearchLimit)

				e.logger.Info("search MB sub-call",
					"entity", "releaseGroups",
					"elapsed", time.Since(t).Round(time.Millisecond),
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
				result.ReleaseGroups = rgs
				mu.Unlock()
			},
		},
		{
			name: "recordings",
			fn: func() {
				t := time.Now()
				recs, err := e.mb.SearchRecordings(mbCtx, query, mbSearchLimit)

				e.logger.Info("search MB sub-call",
					"entity", "recordings",
					"elapsed", time.Since(t).Round(time.Millisecond),
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
				result.Recordings = recs
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

	p1Dur := time.Since(p1Start)

	e.logger.Info("search phase 1 complete (MB)",
		"query", query,
		"artists", len(result.Artists),
		"releaseGroups", len(result.ReleaseGroups),
		"recordings", len(result.Recordings),
		"elapsed", p1Dur.Round(time.Millisecond),
	)

	// Phases 2+3: when the index is ready, use cached popularity
	// from the index to rerank MB results (no API calls).
	// When the index isn't ready, fall back to live LB API calls.
	p2Start := time.Now()
	indexReady := e.index.IsReady()

	if indexReady {
		// Phase 2 (lite): rerank MB results using index popularity.
		e.boostWithIndexPopularity(&result)
	} else {
		// Phase 2: LB popularity lookups (3 POST calls, rate-limited).
		// Use a tight deadline so a slow LB/MB doesn't stall the search.
		slowCtx, slowCancel := context.WithTimeout(e.ctx, searchSlowPathTimeout)

		lbStart := time.Now()
		e.boostWithPopularity(&result)
		lbDur := time.Since(lbStart)

		// Phase 3: cross-reference artist discographies.
		// Skip if the slow-path budget is already exhausted.
		xrefStart := time.Now()

		if slowCtx.Err() == nil {
			e.crossReferenceAlbums(slowCtx, query, &result)
		}

		xrefDur := time.Since(xrefStart)
		slowCancel()

		e.logger.Info("search slow path breakdown",
			"query", query,
			"lbPopularity", lbDur.Round(time.Millisecond),
			"crossRef", xrefDur.Round(time.Millisecond),
		)
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

	// Collect new entries from index.
	var newArtists []MBArtist

	var newRGs []MBReleaseGroup

	var newRecs []MBRecording

	for _, h := range hits {
		switch h.EntityType {
		case "artist":
			if !artistMBIDs[h.MBID] {
				newArtists = append(newArtists, MBArtist{
					MBID:  h.MBID,
					Name:  h.Title,
					Score: scalePopularity(h.Popularity),
				})

				artistMBIDs[h.MBID] = true
			}

		case "release_group":
			if !rgMBIDs[h.MBID] {
				rg := MBReleaseGroup{
					MBID:         h.MBID,
					Title:        h.Title,
					ArtistCredit: h.ArtistName,
				}

				// Extract type from extra_json if available.
				if h.ExtraJSON != "" {
					var extra map[string]string
					if err := json.Unmarshal([]byte(h.ExtraJSON), &extra); err == nil {
						rg.PrimaryType = extra["type"]
					}
				}

				newRGs = append(newRGs, rg)

				rgMBIDs[h.MBID] = true
			}

		case "recording":
			if !recMBIDs[h.MBID] {
				newRecs = append(newRecs, MBRecording{
					MBID:         h.MBID,
					Title:        h.Title,
					ArtistCredit: h.ArtistName,
					Score:        scalePopularity(h.Popularity),
				})

				recMBIDs[h.MBID] = true
			}
		}
	}

	// Prepend index hits so they appear first.
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

// filterAndCap removes low-scoring results, special-purpose
// MusicBrainz artists, and limits each entity slice to maxResults.
func filterAndCap(result *MBSearchResult) {
	// Filter artists by minimum blended score and remove SPAs.
	if len(result.Artists) > 0 {
		filtered := result.Artists[:0]

		for _, a := range result.Artists {
			if a.Score >= minBlendedScore && !mbSpecialPurposeArtists[a.MBID] {
				filtered = append(filtered, a)
			}
		}

		result.Artists = filtered
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
	// Blending weights for final score.
	relevanceWeight  = 0.4
	popularityWeight = 0.6

	// mbSearchLimit is passed to each MB search call.  Slightly
	// larger than maxResults to allow headroom for filtering.
	mbSearchLimit = 20

	// searchMBTimeout is the maximum time to wait for MusicBrainz
	// API responses during interactive search.  If MB is slow,
	// results degrade to index-only rather than blocking the user.
	searchMBTimeout = 4 * time.Second

	// searchSlowPathTimeout caps the total time spent on the slow
	// path (LB popularity + cross-referencing).  When the index
	// isn't ready, these API calls can stack up — especially
	// cross-referencing, which browses 3 artist discographies via
	// MB and can hit 429 retries.  The timeout ensures search
	// returns within a reasonable window.
	searchSlowPathTimeout = 3 * time.Second

	// maxResults caps each entity slice after filtering.
	maxResults = 15

	// minBlendedScore is the floor for artists and recordings
	// after popularity reranking (0–100 scale).
	minBlendedScore = 25
)

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
	popMap := e.index.GetPopularityBatch(allMBIDs)
	if popMap == nil {
		return
	}

	// Build per-entity maps from the batch result.
	artistPop := make(map[string]int, len(result.Artists))
	for _, a := range result.Artists {
		if pop, ok := popMap[a.MBID]; ok {
			artistPop[a.MBID] = pop
		}
	}

	rerankArtists(result.Artists, artistPop)

	rgPop := make(map[string]int, len(result.ReleaseGroups))
	for _, rg := range result.ReleaseGroups {
		if pop, ok := popMap[rg.MBID]; ok {
			rgPop[rg.MBID] = pop
		}
	}

	rerankReleaseGroups(result.ReleaseGroups, rgPop)

	recPop := make(map[string]int, len(result.Recordings))
	for _, r := range result.Recordings {
		if pop, ok := popMap[r.MBID]; ok {
			recPop[r.MBID] = pop
		}
	}

	rerankRecordings(result.Recordings, recPop)
}

// boostWithPopularity fetches ListenBrainz listen counts for all
// entities in result and re-sorts each slice using a blended score
// of MB text relevance + log-scaled popularity.  Modifies result
// in place.  Failures are logged and degrade to MB-only ordering.
func (e *Service) boostWithPopularity(result *MBSearchResult) {
	// Collect MBIDs per entity type.
	artistMBIDs := make([]string, len(result.Artists))
	for i, a := range result.Artists {
		artistMBIDs[i] = a.MBID
	}

	recordingMBIDs := make([]string, len(result.Recordings))
	for i, r := range result.Recordings {
		recordingMBIDs[i] = r.MBID
	}

	rgMBIDs := make([]string, len(result.ReleaseGroups))
	for i, rg := range result.ReleaseGroups {
		rgMBIDs[i] = rg.MBID
	}

	// Fetch popularity concurrently.
	var (
		artistPop    map[string]int
		recordingPop map[string]int
		rgPop        map[string]int
		wg           sync.WaitGroup
	)

	wg.Add(3) //nolint:mnd

	go func() {
		defer wg.Done()

		pop, err := e.lb.ArtistPopularity(e.ctx, artistMBIDs)
		if err != nil {
			e.logger.Warn("popularity lookup failed", "entity", "artist", "error", err)

			return
		}

		artistPop = pop
	}()

	go func() {
		defer wg.Done()

		pop, err := e.lb.RecordingPopularity(e.ctx, recordingMBIDs)
		if err != nil {
			e.logger.Warn("popularity lookup failed", "entity", "recording", "error", err)

			return
		}

		recordingPop = pop
	}()

	go func() {
		defer wg.Done()

		pop, err := e.lb.ReleaseGroupPopularity(e.ctx, rgMBIDs)
		if err != nil {
			e.logger.Warn("popularity lookup failed", "entity", "releaseGroup", "error", err)

			return
		}

		rgPop = pop
	}()

	wg.Wait()

	// Rerank each entity type.
	rerankArtists(result.Artists, artistPop)
	rerankRecordings(result.Recordings, recordingPop)
	rerankReleaseGroups(result.ReleaseGroups, rgPop)
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

	// Boost artists whose name contains the full query.
	if len(result.Artists) > 1 {
		sort.SliceStable(result.Artists, func(i, j int) bool {
			iMatch := nameMatchTier(q, strings.ToLower(result.Artists[i].Name))
			jMatch := nameMatchTier(q, strings.ToLower(result.Artists[j].Name))

			return iMatch < jMatch
		})

		// For same-named artists in tier 0, resolve ordering via
		// a targeted LB popularity lookup.  This handles the case
		// where multiple artists share a name (e.g. "The Teenagers"
		// US vs FR) and the index has no popularity for either.
		e.disambiguateSameNameArtists(q, result.Artists)
	}

	// Boost release groups whose title contains the full query.
	if len(result.ReleaseGroups) > 1 {
		sort.SliceStable(result.ReleaseGroups, func(i, j int) bool {
			iMatch := nameMatchTier(q, strings.ToLower(result.ReleaseGroups[i].Title))
			jMatch := nameMatchTier(q, strings.ToLower(result.ReleaseGroups[j].Title))

			if iMatch != jMatch {
				return iMatch < jMatch
			}

			return false
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
		return pop[artists[i].MBID] > pop[artists[j].MBID]
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

// rerankArtists sorts artists by blended score and updates their
// Score field to the new value (0–100 scale).
func rerankArtists(artists []MBArtist, pop map[string]int) {
	if len(artists) == 0 {
		return
	}

	maxPop := maxListenCount(pop)

	sort.SliceStable(artists, func(i, j int) bool {
		si := blendedScore(float64(artists[i].Score)/100.0, pop[artists[i].MBID], maxPop)
		sj := blendedScore(float64(artists[j].Score)/100.0, pop[artists[j].MBID], maxPop)

		return si > sj
	})

	// Update Score field so the frontend's top-results section can
	// use it directly.
	maxPop2 := maxListenCount(pop)

	for i := range artists {
		s := blendedScore(float64(artists[i].Score)/100.0, pop[artists[i].MBID], maxPop2)
		artists[i].Score = int(s * 100)
	}
}

// rerankRecordings sorts recordings by blended score and updates
// their Score field.
func rerankRecordings(recordings []MBRecording, pop map[string]int) {
	if len(recordings) == 0 {
		return
	}

	maxPop := maxListenCount(pop)

	sort.SliceStable(recordings, func(i, j int) bool {
		si := blendedScore(float64(recordings[i].Score)/100.0, pop[recordings[i].MBID], maxPop)
		sj := blendedScore(float64(recordings[j].Score)/100.0, pop[recordings[j].MBID], maxPop)

		return si > sj
	})

	for i := range recordings {
		s := blendedScore(float64(recordings[i].Score)/100.0, pop[recordings[i].MBID], maxPop)
		recordings[i].Score = int(s * 100)
	}
}

// rerankReleaseGroups sorts release groups by popularity only
// (they have no MB score field).
func rerankReleaseGroups(rgs []MBReleaseGroup, pop map[string]int) {
	if len(rgs) == 0 || len(pop) == 0 {
		return
	}

	sort.SliceStable(rgs, func(i, j int) bool {
		return pop[rgs[i].MBID] > pop[rgs[j].MBID]
	})
}

// blendedScore computes relevanceWeight*relevance + popularityWeight*logPop.
// relevance is 0–1. listenCount is raw; maxListenCount is the
// maximum in the result set (for normalization).
func blendedScore(relevance float64, listenCount, maxListenCount int) float64 {
	if maxListenCount <= 0 {
		return relevance
	}

	logPop := math.Log10(float64(listenCount)+1) / math.Log10(float64(maxListenCount)+1)

	return relevanceWeight*relevance + popularityWeight*logPop
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
