package explore

import (
	"context"
	"log/slog"
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
// groups, and recordings matching the query, returning aggregated
// results in a single round-trip.  If any sub-search fails the
// error is logged and the remaining results are still returned.
func (e *Service) Search(query string) (*MBSearchResult, error) {
	e.logger.Info("search started", "query", query)

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

	e.logger.Info("search completed",
		"query", query,
		"artists", len(result.Artists),
		"releaseGroups", len(result.ReleaseGroups),
		"recordings", len(result.Recordings),
	)

	return &result, nil
}
