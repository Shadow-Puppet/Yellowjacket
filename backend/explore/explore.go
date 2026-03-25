package explore

import (
	"context"
	"log/slog"
	"math"
	"sort"
	"sync"

	"yellowjacket/backend/database"
)

// Service is the Wails-bound service for the explore feature.
// It owns the lifecycle of all explore-related components: the
// MusicBrainz client, ListenBrainz client, rate limiter, and
// response cache.  Its exported methods form the binding surface
// that the frontend calls via generated TypeScript stubs.
type Service struct {
	mb     *MusicBrainzClient
	lb     *ListenBrainzClient
	cache  *Cache
	logger *slog.Logger
	ctx    context.Context
}

// NewExploreService creates a Service backed by the given
// database.  It instantiates the rate limiter, cache, MusicBrainz
// client, and ListenBrainz client internally.
func NewExploreService(logger *slog.Logger, db *database.DB) *Service {
	cache := NewCache(db, logger.WithGroup("cache"))
	limiter := NewRateLimiter()
	mb := NewMusicBrainzClient(cache, logger.WithGroup("musicbrainz"))
	lb := NewListenBrainzClient(limiter, cache, logger.WithGroup("listenbrainz"))

	logger.Info("explore service created")

	return &Service{
		mb:     mb,
		lb:     lb,
		cache:  cache,
		logger: logger,
		ctx:    context.Background(),
	}
}

// SetContext injects the Wails runtime context.  Called from
// OnStartup after the Wails runtime is initialised.
func (e *Service) SetContext(ctx context.Context) {
	e.ctx = ctx
}

// ---------------------------------------------------------------------------
// MusicBrainz search
// ---------------------------------------------------------------------------

// SearchArtists queries MusicBrainz for artists matching the query.
func (e *Service) SearchArtists(query string) ([]MBArtist, error) {
	return e.mb.SearchArtists(e.ctx, query, 0)
}

// SearchReleaseGroups queries MusicBrainz for release groups matching the query.
func (e *Service) SearchReleaseGroups(query string) ([]MBReleaseGroup, error) {
	return e.mb.SearchReleaseGroups(e.ctx, query, 0)
}

// SearchRecordings queries MusicBrainz for recordings matching the query.
func (e *Service) SearchRecordings(query string) ([]MBRecording, error) {
	return e.mb.SearchRecordings(e.ctx, query, 0)
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
func (e *Service) BrowseReleaseGroups(artistMBID string) ([]MBReleaseGroup, error) {
	return e.mb.BrowseReleaseGroups(e.ctx, artistMBID)
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

// Search concurrently queries MusicBrainz for artists, release
// groups, and recordings matching the query, then boosts results
// using ListenBrainz popularity data.  The final score blends
// text relevance (60%) with log-scaled listen counts (40%).
//
// If any sub-search or popularity lookup fails the error is logged
// and the remaining results are still returned — popularity
// failures degrade to MB-only ordering.
func (e *Service) Search(query string) (*MBSearchResult, error) {
	e.logger.Info("search started", "query", query)

	// Phase 1: concurrent MB search (3 goroutines, library-limited).
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
				artists, err := e.mb.SearchArtists(e.ctx, query, 0)
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
				rgs, err := e.mb.SearchReleaseGroups(e.ctx, query, 0)
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
				recs, err := e.mb.SearchRecordings(e.ctx, query, 0)
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

	e.logger.Info("search MB complete",
		"query", query,
		"artists", len(result.Artists),
		"releaseGroups", len(result.ReleaseGroups),
		"recordings", len(result.Recordings),
	)

	// Phase 2: concurrent LB popularity lookups (3 goroutines,
	// rate-limited).  Each hits a different endpoint so they can
	// overlap on different rate-limiter tokens.
	e.boostWithPopularity(&result)

	e.logger.Info("search completed",
		"query", query,
		"artists", len(result.Artists),
		"releaseGroups", len(result.ReleaseGroups),
		"recordings", len(result.Recordings),
	)

	return &result, nil
}

// ---------------------------------------------------------------------------
// Popularity-boosted reranking
// ---------------------------------------------------------------------------

const (
	// Blending weights for final score.
	relevanceWeight  = 0.6
	popularityWeight = 0.4
)

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
