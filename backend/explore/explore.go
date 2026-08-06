package explore

import (
	"context"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sync/singleflight"

	"yellowjacket/backend/database"
	"yellowjacket/backend/events"
	"yellowjacket/backend/jobs"
)

// Service is the Wails-bound service for the explore feature.
// It owns the lifecycle of all explore-related components: the
// MusicBrainz client, ListenBrainz client, rate limiter, and
// response cache.  Its exported methods form the binding surface
// that the frontend calls via generated TypeScript stubs.
type Service struct {
	mb         *MusicBrainzClient
	lb         *ListenBrainzClient
	lrclib     *LRCLibClient
	cache      *Cache
	index      *SearchIndex
	artProxy   *CoverArtProxy
	artistImg  *ArtistImageProvider
	libMBID    *LibraryMBIDIndex
	caaLimiter *RateLimiter
	db         *database.DB
	logger     *slog.Logger
	ctx        context.Context

	// searchMu guards searchCancel, which cancels the currently
	// in-flight SearchLocal so a superseded query releases the shared
	// SQLite connection immediately instead of running to completion.
	searchMu     sync.Mutex
	searchCancel context.CancelFunc

	// discogSF collapses concurrent lazy discography fetches for the same
	// artist (the detail page fires top-tracks and top-releases at once)
	// into a single background fetch + one ArtistDiscographyReady event.
	discogSF singleflight.Group

	// similarSF collapses concurrent lazy similar-artist fetches for the
	// same artist into a single LB labs call + one ArtistSimilarReady event.
	similarSF singleflight.Group

	// releasesSF collapses concurrent lazy BrowseReleases fetches for the
	// same release group (e.g. the album page firing while a prefetch is
	// already in flight) into one MusicBrainz browse + one
	// AlbumReleasesReady event.
	releasesSF singleflight.Group
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
	lrclib := NewLRCLibClient(cache, logger.WithGroup("lrclib"))
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
		lrclib:     lrclib,
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

// RefreshListenCounts folds any newly-published incremental listen dumps
// into the index's popularity numbers, in the background.  No-op when
// offline, when a full build is running, when there is no baseline
// import, or when the last refresh was within the weekly cadence.  Fully
// local — downloads the small daily dumps but makes no ListenBrainz API
// calls.
func (e *Service) RefreshListenCounts() {
	go e.index.RefreshListenCounts(e.ctx, listensCatchupInterval)
}

// StopIndexBuild cancels the background search index build.
// Call before a full rescan to free the DB for the scan.
func (e *Service) StopIndexBuild() {
	e.index.StopBuild()
}

// CoreCatalogImported reports whether a prebuilt catalog artifact has
// been merged into this index.
func (e *Service) CoreCatalogImported() bool {
	return e.index.artifactAlreadyMerged()
}

// SetJobRegistry wires the background job registry into the search
// index so its build reports progress and controls to the frontend.
func (e *Service) SetJobRegistry(reg *jobs.Registry) {
	e.index.SetJobRegistry(reg)
}

// AdoptPausedIndexBuild re-registers a build paused in a previous
// session so it appears in the jobs panel, still paused.
func (e *Service) AdoptPausedIndexBuild() {
	e.index.AdoptPausedBuild()
}

// IndexImportComplete reports whether the dump import has finished all
// of its stages.  Distinct from IsIndexReady, which only means the index
// holds enough rows to answer queries — a partially imported index is
// ready but not complete.  Used by the headless builder to decide
// whether another run is needed.
func (e *Service) IndexImportComplete() bool {
	return e.index.ImportComplete()
}

// IndexBaselineSeries returns the incremental listens series the index's
// popularity is caught up to.  A change across a refresh means new data
// was folded in.
func (e *Service) IndexBaselineSeries() int {
	return e.index.BaselineSeries()
}

// IndexLastImported returns when the dump import last completed, or the
// zero time if it never has.
func (e *Service) IndexLastImported() time.Time {
	return e.index.LastImported()
}

// PrepareIndexRebuild clears the completion marker so the next build
// re-imports from the newest published dump.
func (e *Service) PrepareIndexRebuild() {
	e.index.PrepareRebuild()
}

// RefreshIndexNow folds newly published incremental listens dumps into
// the index synchronously.  Pass 0 to bypass the cadence gate.
func (e *Service) RefreshIndexNow(minInterval time.Duration) {
	e.index.RefreshNow(e.ctx, minInterval)
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

// PopulateLocalCrossReferencesIfNeeded runs the library→index sync only
// when it has not run since the last library change.  Use it on the
// unchanged-library launch path so the write-heavy re-sync is skipped in
// steady state; the scan-completion path calls the unconditional form.
func (e *Service) PopulateLocalCrossReferencesIfNeeded() {
	if e.index.hasMeta(localXrefReadyKey) {
		return
	}

	e.index.PopulateLocalCrossReferences()
}

// BackfillLibraryDiscographies enriches owned artists that have not had
// their discography fetched yet, in the background.  Idempotent and
// bounded — the query only returns unenriched artists and each is marked
// discog_fetched on success, so this is cheap (an empty query) once every
// owned artist is covered and safe to call on every scan and launch.
func (e *Service) BackfillLibraryDiscographies() {
	go e.index.BackfillLibraryDiscographies(e.ctx)
}

// InvalidateLibrarySync clears the "ready" markers guarding the gated
// library-sync steps so they re-run on the next launch.  Call after a
// mutation that changes owned content outside a scan (e.g. removing a
// library), which would otherwise leave stale in_library flags and
// orphaned lyric-index rows.
func (e *Service) InvalidateLibrarySync() {
	e.index.deleteMeta(localXrefReadyKey)
	e.index.deleteMeta(lyricsIndexReadyKey)
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

// beginSearch cancels any SearchLocal still in flight and returns a
// fresh context (plus its cancel func) scoped to the new search.
// Callers must defer the returned cancel so the context's resources
// are released once the search returns.
func (e *Service) beginSearch() (context.Context, context.CancelFunc) {
	e.searchMu.Lock()
	defer e.searchMu.Unlock()

	if e.searchCancel != nil {
		e.searchCancel()
	}

	ctx, cancel := context.WithCancel(e.ctx)
	e.searchCancel = cancel

	return ctx, cancel
}

// SearchLocal queries only the local FTS5 index and returns fully
// ranked results instantly with no network calls.  This is the
// primary interactive search path: now that the index is populated
// from the MetaBrainz dumps it covers essentially every popular
// entity, so the frontend drives search entirely from here.  The
// old MusicBrainz network pipeline (Search) is retained for a future
// opt-in "search online" affordance but is no longer called on the
// hot path.
//
// Returns nil if the index has no hits for the query, so the caller
// can fall back to whatever owned-library matches it already has.
func (e *Service) SearchLocal(query string) *MBSearchResult {
	searchStart := time.Now()

	// Abandon any search still running: the query has changed, so its
	// results are already stale.  Cancelling interrupts its in-flight
	// FTS query and frees the single shared SQLite connection for this
	// one, instead of letting superseded searches back up behind it.
	ctx, cancel := e.beginSearch()
	defer cancel()

	sqlStart := time.Now()
	indexHits := e.index.Search(ctx, query, indexSearchLimit)
	sqlDur := time.Since(sqlStart)

	// Superseded mid-query: drop this result so the caller's version
	// check isn't racing a partial one, and skip the post-processing.
	if ctx.Err() != nil {
		return nil
	}

	if len(indexHits) == 0 {
		e.logger.Info("searchlocal complete (no hits)",
			"query", query,
			"sql", sqlDur.Round(time.Microsecond),
			"total", time.Since(searchStart).Round(time.Microsecond),
		)

		return nil
	}

	postStart := time.Now()

	var result MBSearchResult

	mergeIndexHits(query, &result, indexHits)
	e.resolveRecordingReleaseGroups(result.Recordings)

	mergeDur := time.Since(postStart)

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

	// Boost exact/substring name matches so a precise query ranks the
	// obvious entity first, mirroring the MB pipeline's Phase 5.
	boostStart := time.Now()

	e.boostNameMatches(query, &result)

	boostDur := time.Since(boostStart)

	// Cap counts but skip the minBlendedScore filter: this path has no
	// MB results to calibrate against, so we surface whatever local
	// matches exist rather than dropping low-popularity ones.
	if len(result.Artists) > maxResults {
		result.Artists = result.Artists[:maxResults]
	}

	if len(result.ReleaseGroups) > maxResults {
		result.ReleaseGroups = result.ReleaseGroups[:maxResults]
	}

	if len(result.Recordings) > maxResults {
		result.Recordings = result.Recordings[:maxResults]
	}

	// Resolve the top-result cards via intent scoring (index-only —
	// no network), same as the MB pipeline's Phase 7.
	topStart := time.Now()
	result.TopResults = e.resolveTopResults(query, &result)
	topDur := time.Since(topStart)

	postDur := time.Since(postStart)

	e.logger.Info("searchlocal complete",
		"query", query,
		"hits", len(indexHits),
		"artists", len(result.Artists),
		"releaseGroups", len(result.ReleaseGroups),
		"recordings", len(result.Recordings),
		"sql", sqlDur.Round(time.Microsecond),
		"post", postDur.Round(time.Microsecond),
		"post.merge", mergeDur.Round(time.Microsecond),
		"post.boost", boostDur.Round(time.Microsecond),
		"post.topResults", topDur.Round(time.Microsecond),
		"total", time.Since(searchStart).Round(time.Microsecond),
	)

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
			ArtistMBID:       indexed.ArtistMBID,
			Popularity:       indexed.Popularity,
			ListenerCount:    indexed.ListenerCount,
			PrimaryType:      indexed.PrimaryType,
			SecondaryTypes:   secondary,
			FirstReleaseDate: indexed.ReleaseDate,
			InLibrary:        indexed.InLibrary || indexed.LocalReleaseGroupID > 0,
			LocalID:          indexed.LocalReleaseGroupID,
		}

		// Background: fetch full MB data once per RG to fill in fields
		// the dump doesn't carry (notably secondary_types, used by the
		// album page's version scorer).  Gated on DiscogFetched — not on
		// "secondary_types == ''" — so a genuinely studio album (which
		// has no secondary types) is looked up once and marked, instead
		// of re-firing on every page visit.  The result is written back
		// to the index so the next visit reads it locally.
		if !indexed.DiscogFetched {
			go func() {
				fetched, err := e.mb.LookupReleaseGroup(e.ctx, mbid)
				if err == nil && fetched != nil {
					e.index.PersistReleaseGroupLookup(fetched)
				}
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
				ArtistMBID:       r.ArtistMBID,
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

	// Not indexed yet.  Don't block on a live MusicBrainz browse: the top
	// sections already trigger EnsureArtistDiscography (via
	// TopReleaseGroupsForArtist), which fetches the same release groups
	// from ListenBrainz and writes them into the index.  Kick that off (or
	// join the in-flight fetch) and return empty — the ArtistDiscographyReady
	// event signals the caller to re-read this section from the index.
	e.ensureDiscographyAsync(artistMBID)

	return nil, nil
}

// resolveArtistName picks the best available artist name for a list
// of release groups returned from MB browse-by-artist.  MB browse
// doesn't echo back the artist credit on each item (since the artist
// is the query parameter), so we need to find a name from somewhere:
//  1. First non-empty ArtistCredit on any release group
//  2. The local explore_index (if the artist was previously indexed)
//  3. A LookupArtist call to MB (last resort)
//  4. The MBID itself (worst case fallback)
//
// looksLikeFeaturingCredit reports whether a credit string carries a
// "featuring" clause — i.e. it names a collaboration rather than a
// single artist.  Only true "featuring" markers count; "&", "x", and
// "," are excluded because they appear inside real artist names.
func looksLikeFeaturingCredit(credit string) bool {
	lower := strings.ToLower(credit)
	for _, sep := range []string{" feat. ", " feat ", " featuring ", " ft. ", " ft "} {
		if strings.Contains(lower, sep) {
			return true
		}
	}

	return false
}

func (e *Service) resolveArtistName(artistMBID string, rgs []MBReleaseGroup) string {
	// Try the first single-artist credit from the release groups.  A
	// credit carrying a "featuring" clause names a collaboration, not
	// the artist whose page this is, so skip those and fall through to
	// the index / MB lookup for the canonical single-artist name — this
	// is what AddFromCache stamps onto every release group.
	for _, rg := range rgs {
		if rg.ArtistCredit != "" && !looksLikeFeaturingCredit(rg.ArtistCredit) {
			return rg.ArtistCredit
		}
	}

	// Check the index for a previously-indexed artist row.
	if indexed := e.index.LookupArtistByMBID(
		artistMBID,
	); indexed != nil && indexed.Title != "" {
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
//
// Local-first, non-blocking: a warm response cache is served instantly;
// on a miss the request does NOT block on a live MusicBrainz browse
// (which pulls every version's full tracklist and can take seconds).
// Instead it kicks off a background fetch and returns empty — the
// AlbumReleasesReady event signals the caller to re-fetch once the cache
// is warm.
func (e *Service) BrowseReleases(releaseGroupMBID string) ([]MBRelease, error) {
	if releases, ok := e.mb.BrowseReleasesCached(releaseGroupMBID); ok {
		e.markReleasesInLibrary(releases)

		return releases, nil
	}

	e.ensureReleasesAsync(releaseGroupMBID)

	return nil, nil
}

// markReleasesInLibrary collects all recording MBIDs across all releases
// and checks them against the local library in a single query, setting the
// InLibrary flag on each track so the tracklist renderer can show the
// library-status indicator without a per-track roundtrip.
func (e *Service) markReleasesInLibrary(releases []MBRelease) {
	var trackMBIDs []string

	for _, rel := range releases {
		for _, t := range rel.Tracks {
			if t.MBID != "" {
				trackMBIDs = append(trackMBIDs, t.MBID)
			}
		}
	}

	if len(trackMBIDs) == 0 {
		return
	}

	found := e.libMBID.CheckMBIDs(trackMBIDs)

	for i := range releases {
		for j := range releases[i].Tracks {
			if _, ok := found[releases[i].Tracks[j].MBID]; ok {
				releases[i].Tracks[j].InLibrary = true
			}
		}
	}
}

// ensureReleasesAsync fetches a release group's releases + tracklists into
// the response cache in the background and emits AlbumReleasesReady when
// done.  Concurrent calls for the same release group (e.g. a prefetch
// already in flight when the user opens the album) collapse into one
// MusicBrainz browse + one event via the singleflight.
func (e *Service) ensureReleasesAsync(releaseGroupMBID string) {
	if releaseGroupMBID == "" {
		return
	}

	go func() {
		_, _, _ = e.releasesSF.Do(releaseGroupMBID, func() (any, error) {
			_, err := e.mb.BrowseReleases(e.ctx, releaseGroupMBID)
			if err == nil && e.ctx != nil {
				runtime.EventsEmit(e.ctx, events.AlbumReleasesReady, releaseGroupMBID)
			}

			return nil, nil
		})
	}()
}

// PrefetchReleases warms the local response cache for a set of release
// groups in the background so opening any of them is instant.  Called from
// the artist page once its top-releases / discography render — album
// navigation almost always originates there.  Already-cached groups are
// skipped; a cap bounds how many live fetches a single artist view can
// trigger so the MusicBrainz rate limiter isn't flooded.
func (e *Service) PrefetchReleases(releaseGroupMBIDs []string) {
	const maxPrefetch = 8

	fired := 0

	for _, mbid := range releaseGroupMBIDs {
		if mbid == "" {
			continue
		}

		if _, ok := e.mb.BrowseReleasesCached(mbid); ok {
			continue
		}

		e.ensureReleasesAsync(mbid)

		fired++
		if fired >= maxPrefetch {
			return
		}
	}
}

// ---------------------------------------------------------------------------
// ListenBrainz
// ---------------------------------------------------------------------------

// topRecordingsToWire projects indexed recordings to the wire type.
func topRecordingsToWire(indexed []SearchIndexResult) []LBTopRecording {
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

	return out
}

// TopRecordingsForArtist returns the most-listened recordings for an
// artist.  Serves instantly from the local index when available; when the
// artist isn't indexed yet it returns empty immediately and fetches the
// discography in the background, emitting ArtistDiscographyReady so the
// caller can re-fetch — the request never blocks on a live fetch.
func (e *Service) TopRecordingsForArtist(artistMBID string) ([]LBTopRecording, error) {
	if indexed := e.index.TopRecordingsByArtist(artistMBID, 50); len(indexed) > 0 {
		out := topRecordingsToWire(indexed)
		e.resolveTopRecordingReleaseGroups(out)

		return out, nil
	}

	e.ensureDiscographyAsync(artistMBID)

	return nil, nil
}

// resolveTopRecordingReleaseGroups fills each top recording's parent
// release group (from its CAA release MBID) in a single local index
// query, so a top-track row can link to its album page with the track
// highlighted.
func (e *Service) resolveTopRecordingReleaseGroups(recs []LBTopRecording) {
	var caaMBIDs []string

	for i := range recs {
		if recs[i].CAAReleaseMBID != "" {
			caaMBIDs = append(caaMBIDs, recs[i].CAAReleaseMBID)
		}
	}

	if len(caaMBIDs) == 0 {
		return
	}

	rgByCAA := e.index.ReleaseGroupMBIDsForCAAReleaseMBIDs(caaMBIDs)

	for i := range recs {
		if rg, ok := rgByCAA[recs[i].CAAReleaseMBID]; ok {
			recs[i].ReleaseGroupMBID = rg
		}
	}
}

// topReleaseGroupsToWire projects indexed release groups to the wire type.
func topReleaseGroupsToWire(indexed []SearchIndexResult) []LBTopReleaseGroup {
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

	return out
}

// TopReleaseGroupsForArtist returns the most-listened release groups for
// an artist.  Same non-blocking contract as TopRecordingsForArtist: index
// hit is instant, a miss kicks off a background discography fetch and
// returns empty, and ArtistDiscographyReady signals when to re-fetch.
func (e *Service) TopReleaseGroupsForArtist(artistMBID string) ([]LBTopReleaseGroup, error) {
	if indexed := e.index.TopReleaseGroupsByArtist(artistMBID, 50); len(indexed) > 0 {
		return topReleaseGroupsToWire(indexed), nil
	}

	e.ensureDiscographyAsync(artistMBID)

	return nil, nil
}

// ensureDiscographyAsync fetches an artist's discography into the index in
// the background and emits ArtistDiscographyReady when done.  Concurrent
// calls for the same artist collapse into one fetch and one event via the
// singleflight, so the detail page firing both top sections at once costs
// a single ListenBrainz round-trip.
func (e *Service) ensureDiscographyAsync(artistMBID string) {
	if artistMBID == "" {
		return
	}

	go func() {
		_, _, _ = e.discogSF.Do(artistMBID, func() (any, error) {
			e.index.EnsureArtistDiscography(e.ctx, artistMBID)

			if e.ctx != nil {
				runtime.EventsEmit(e.ctx, events.ArtistDiscographyReady, artistMBID)
			}

			return nil, nil
		})
	}()
}

// SimilarArtists returns artists similar to the given artist MBID.
func (e *Service) SimilarArtists(artistMBID string) ([]LBSimilarArtist, error) {
	// Try the pre-computed similar_artist_map first (instant, no API call).
	// Populated by the dump patch pass for library artists and, on first
	// view, by the lazy persist below for everyone else.
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

	// Not cached — don't block on the live LB labs call.  Fetch and persist
	// in the background, then emit ArtistSimilarReady so the caller re-reads
	// this section from similar_artist_map.  Later views are served locally.
	e.ensureSimilarArtistsAsync(artistMBID)

	return nil, nil
}

// ensureSimilarArtistsAsync fetches an artist's similar artists from the LB
// labs API into similar_artist_map in the background and emits
// ArtistSimilarReady when done.  Concurrent calls for the same artist
// collapse into one fetch and one event via the singleflight.
func (e *Service) ensureSimilarArtistsAsync(artistMBID string) {
	if artistMBID == "" {
		return
	}

	go func() {
		_, _, _ = e.similarSF.Do(artistMBID, func() (any, error) {
			similar, err := e.lb.SimilarArtists(e.ctx, artistMBID)
			if err == nil {
				e.index.PersistSimilarArtists(artistMBID, similar)

				if e.ctx != nil {
					runtime.EventsEmit(e.ctx, events.ArtistSimilarReady, artistMBID)
				}
			}

			return nil, nil
		})
	}()
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
// or Wikimedia fetch.  Returns "" if not cached.
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

// ---------------------------------------------------------------------------
// Index result merging
// ---------------------------------------------------------------------------

// Index-hit relevance approximation tiers.  MB results derive their
// relevance from MusicBrainz's Lucene score; index hits have none, so
// we approximate it from how cleanly the query matches the title or
// artist credit.  The floor is non-zero because the FTS index already
// matched *something* (a per-word or alias hit).
const (
	indexRelevanceFloor = 0.15
	indexRelExact       = 1.0
	indexRelPrefix      = 0.8
	indexRelWord        = 0.6
	indexRelSubstring   = 0.4
)

// mergeIndexHits injects local popularity index results into the
// MBSearchResult.  Index hits for entity types not already present (by
// MBID) are scored on the SAME blended scale as the reranked MB results
// (text relevance + log-popularity + personalization), then merged and
// re-sorted by that score.  This replaces an earlier approach that
// scored index hits with a half-scaled popularity number and blindly
// prepended them — two incompatible scales that the downstream
// minBlendedScore filter and final sort then compared as if equal.
func mergeIndexHits(query string, result *MBSearchResult, hits []SearchIndexResult) {
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

	// Per-entity-type max popularity over the combined population
	// (existing MB results + incoming index hits) so the blended score
	// normalizes popularity consistently across both sources.  This max
	// may differ slightly from the per-list max the MB rerank used; the
	// single comparable scale is worth that minor drift.
	maxArtistPop, maxRGPop, maxRecPop := 0, 0, 0

	for _, a := range result.Artists {
		maxArtistPop = max(maxArtistPop, a.Popularity)
	}

	for _, rg := range result.ReleaseGroups {
		maxRGPop = max(maxRGPop, rg.Popularity)
	}

	for _, r := range result.Recordings {
		maxRecPop = max(maxRecPop, r.Popularity)
	}

	for _, h := range hits {
		switch h.EntityType {
		case "artist":
			maxArtistPop = max(maxArtistPop, h.Popularity)
		case "release_group":
			maxRGPop = max(maxRGPop, h.Popularity)
		case "recording":
			maxRecPop = max(maxRecPop, h.Popularity)
		}
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
				inLib := h.InLibrary || h.LocalArtistID > 0

				newArtists = append(newArtists, MBArtist{
					MBID:           h.MBID,
					Name:           h.Title,
					Type:           h.ArtistType,
					Country:        h.Country,
					Disambiguation: h.Disambiguation,
					SortName:       h.SortName,
					Score: indexHitBlendedScore(
						query, h.Title, "", h.Popularity, maxArtistPop, inLib, h.IsSimilar,
					),
					HasPopularity: h.Popularity > 0,
					Popularity:    h.Popularity,
					ListenerCount: h.ListenerCount,
					InLibrary:     inLib,
					LocalID:       h.LocalArtistID,
				})

				artistMBIDs[h.MBID] = true
			}

		case "release_group":
			if !rgMBIDs[h.MBID] {
				inLib := h.InLibrary || h.LocalReleaseGroupID > 0

				var secondary []string
				if h.SecondaryTypes != "" {
					secondary = strings.Split(h.SecondaryTypes, ",")
				}

				newRGs = append(newRGs, MBReleaseGroup{
					MBID:         h.MBID,
					Title:        h.Title,
					ArtistCredit: h.ArtistName,
					ArtistMBID:   h.ArtistMBID,
					Score: indexHitBlendedScore(
						query, h.Title, h.ArtistName, h.Popularity, maxRGPop, inLib, h.IsSimilar,
					),
					Popularity:       h.Popularity,
					ListenerCount:    h.ListenerCount,
					PrimaryType:      h.PrimaryType,
					SecondaryTypes:   secondary,
					FirstReleaseDate: h.ReleaseDate,
					InLibrary:        inLib,
					LocalID:          h.LocalReleaseGroupID,
				})
				rgMBIDs[h.MBID] = true
			}

		case "recording":
			if !recMBIDs[h.MBID] {
				inLib := h.InLibrary || h.LocalRecordingID > 0

				newRecs = append(newRecs, MBRecording{
					MBID:         h.MBID,
					Title:        h.Title,
					Length:       h.Duration,
					ArtistCredit: h.ArtistName,
					ArtistMBID:   h.ArtistMBID,
					Score: indexHitBlendedScore(
						query, h.Title, h.ArtistName, h.Popularity, maxRecPop, inLib, h.IsSimilar,
					),
					Popularity:     h.Popularity,
					ListenerCount:  h.ListenerCount,
					CAAReleaseMBID: h.CAAReleaseMBID,
					ReleaseName:    h.ReleaseName,
					InLibrary:      inLib,
					LocalID:        h.LocalRecordingID,
				})

				recMBIDs[h.MBID] = true
			}
		}
	}

	// Merge and re-sort each list by the now-comparable blended Score.
	if len(newArtists) > 0 {
		result.Artists = append(result.Artists, newArtists...)
		sort.SliceStable(result.Artists, func(i, j int) bool {
			return result.Artists[i].Score > result.Artists[j].Score
		})
	}

	if len(newRGs) > 0 {
		result.ReleaseGroups = append(result.ReleaseGroups, newRGs...)
		sort.SliceStable(result.ReleaseGroups, func(i, j int) bool {
			return result.ReleaseGroups[i].Score > result.ReleaseGroups[j].Score
		})
	}

	if len(newRecs) > 0 {
		result.Recordings = append(result.Recordings, newRecs...)
		sort.SliceStable(result.Recordings, func(i, j int) bool {
			return result.Recordings[i].Score > result.Recordings[j].Score
		})
	}
}

// indexHitBlendedScore scores a local index hit on the same 0–100
// blended scale the MB rerank uses, so merged results sort and filter
// consistently regardless of source.
func indexHitBlendedScore(
	query, title, artist string,
	pop, maxPop int,
	inLibrary, isSimilar bool,
) int {
	rel := indexHitRelevance(query, title, artist)

	personal := 0.0

	switch {
	case inLibrary:
		personal = personalInLibrary
	case isSimilar:
		personal = personalSimilar
	}

	return int(blendedScoreFull(rel, pop, maxPop, personal) * 100) //nolint:mnd
}

// indexHitRelevance approximates a 0..1 text-relevance for an index hit
// from how cleanly the query matches its title or artist credit, taking
// the stronger of the two fields.
func indexHitRelevance(query, title, artist string) float64 {
	q := normalizeForMatch(query)
	if q == "" {
		return indexRelevanceFloor
	}

	best := indexRelevanceFloor

	for _, field := range [2]string{title, artist} {
		if field == "" {
			continue
		}

		f := normalizeForMatch(field)

		switch {
		case f == q:
			best = max(best, indexRelExact)
		case strings.HasPrefix(f, q):
			best = max(best, indexRelPrefix)
		case containsWord(f, q):
			best = max(best, indexRelWord)
		case strings.Contains(f, q):
			best = max(best, indexRelSubstring)
		}
	}

	return best
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
// Search result limits and thresholds
// ---------------------------------------------------------------------------

const (
	// indexSearchLimit is the number of results to fetch from the local
	// popularity index.  Larger than maxResults because results are
	// filtered and the index is the primary search domain.
	indexSearchLimit = 60

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

// disambiguateSameNameArtists resolves ordering among artists that
// share the exact same name as the query, by popularity descending.
// It runs entirely from the local index — no network: index-sourced
// artist rows already carry their listen count, and a single
// GetPopularityBatch fills any that don't.  Keeping this offline is
// what lets SearchLocal honour its "no network calls" contract; a live
// lookup here previously stalled the hot search path.
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

	// Collect MBIDs so the index can fill popularity for any rows that
	// don't already carry it (e.g. artists sourced from the MB pipeline).
	mbids := make([]string, 0, sameNameEnd)
	for i := range sameNameEnd {
		if artists[i].MBID != "" {
			mbids = append(mbids, artists[i].MBID)
		}
	}

	if len(mbids) < 2 {
		return
	}

	// Index-only popularity lookup — no network.
	var indexPop map[string]int
	if batch := e.index.GetPopularityBatch(mbids); batch != nil {
		indexPop = batch.Popularity
	}

	// Prefer the index popularity, falling back to whatever listen count
	// the artist row already carries when the index has none.
	popOf := func(a MBArtist) int {
		if p, ok := indexPop[a.MBID]; ok && p > 0 {
			return p
		}

		return a.Popularity
	}

	// Re-sort the same-name block by popularity descending.
	sort.SliceStable(artists[:sameNameEnd], func(i, j int) bool {
		return popOf(artists[i]) > popOf(artists[j])
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
	clicksStart := time.Now()
	clicks := e.getSearchClicks(q)
	clicksDur := time.Since(clicksStart)

	exactStart := time.Now()
	exactMatches := e.index.ExactMatches(q, topResultsExactCap)
	exactDur := time.Since(exactStart)

	if clicksDur+exactDur > 100*time.Millisecond {
		e.logger.Info("search top results: slow candidate gather",
			"query", query,
			"getSearchClicks", clicksDur.Round(time.Microsecond),
			"exactMatches", exactDur.Round(time.Microsecond),
		)
	}

	gatherStart := time.Now()
	candidates := e.gatherTopCandidates(q, result, exactMatches, clicks)
	gatherDur := time.Since(gatherStart)

	if gatherDur > 100*time.Millisecond {
		e.logger.Info("search top results: slow gatherTopCandidates",
			"query", query,
			"candidates", len(candidates),
			"elapsed", gatherDur.Round(time.Microsecond),
		)
	}

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

	rgResolveStart := time.Now()

	e.resolveTopResultReleaseGroups(selected)

	if d := time.Since(rgResolveStart); d > 100*time.Millisecond {
		e.logger.Info("search top results: slow RG resolve",
			"query", query,
			"selected", len(selected),
			"elapsed", d.Round(time.Microsecond),
		)
	}

	return selected
}

// resolveTopResultReleaseGroups resolves each recording top-result's parent
// release group (from its CAA release MBID) so a track click can open the
// album page with the track highlighted — the same behaviour as clicking a
// track anywhere else.  Uses a single local index query; recordings whose
// release can't be resolved simply keep an empty ReleaseGroupMBID and fall
// back to a name-only navigation on the frontend.
func (e *Service) resolveTopResultReleaseGroups(results []TopResult) {
	var caaMBIDs []string

	for i := range results {
		if results[i].EntityType == "recording" && results[i].CAAReleaseMBID != "" {
			caaMBIDs = append(caaMBIDs, results[i].CAAReleaseMBID)
		}
	}

	if len(caaMBIDs) == 0 {
		return
	}

	rgByCAA := e.index.ReleaseGroupMBIDsForCAAReleaseMBIDs(caaMBIDs)

	for i := range results {
		if results[i].EntityType != "recording" {
			continue
		}

		if rg, ok := rgByCAA[results[i].CAAReleaseMBID]; ok {
			results[i].ReleaseGroupMBID = rg
		}
	}
}

// resolveRecordingReleaseGroups fills each recording's parent release
// group (from its CAA release MBID) in a single local index query, so
// a track shown in search results can link to its album page with the
// track highlighted.  Recordings whose release can't be resolved keep
// an empty ReleaseGroupMBID and fall back to non-linked text.
func (e *Service) resolveRecordingReleaseGroups(recordings []MBRecording) {
	var caaMBIDs []string

	for i := range recordings {
		if recordings[i].CAAReleaseMBID != "" {
			caaMBIDs = append(caaMBIDs, recordings[i].CAAReleaseMBID)
		}
	}

	if len(caaMBIDs) == 0 {
		return
	}

	rgByCAA := e.index.ReleaseGroupMBIDsForCAAReleaseMBIDs(caaMBIDs)

	for i := range recordings {
		if rg, ok := rgByCAA[recordings[i].CAAReleaseMBID]; ok {
			recordings[i].ReleaseGroupMBID = rg
		}
	}
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
				ArtistMBID:   rg.ArtistMBID,
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
				ArtistMBID:   rg.ArtistMBID,
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
				EntityType:     "recording",
				MBID:           r.MBID,
				Name:           r.Title,
				ArtistCredit:   r.ArtistCredit,
				ArtistMBID:     r.ArtistMBID,
				Length:         r.Length,
				CAAReleaseMBID: r.CAAReleaseMBID,
				ReleaseName:    r.ReleaseName,
				InLibrary:      r.InLibrary,
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
				EntityType:     "recording",
				MBID:           r.MBID,
				Name:           r.Title,
				ArtistCredit:   r.ArtistCredit,
				ArtistMBID:     r.ArtistMBID,
				Length:         r.Length,
				CAAReleaseMBID: r.CAAReleaseMBID,
				ReleaseName:    r.ReleaseName,
				InLibrary:      r.InLibrary,
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
					ArtistMBID:   m.ArtistMBID,
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
					ArtistMBID:   m.ArtistMBID,
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

// normLog normalizes a count to [0,1] against a fixed reference scale,
// so the value is stable across queries (unlike the blended rerank,
// which normalizes against each result set's dynamic max).
func normLog(n int) float64 {
	const maxScale = 50_000_000 // top-tier artists have ~10-150M listens

	return logNormalize(n, maxScale)
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

// blendedScoreFull computes the weighted blend of relevance, popularity,
// and personalization.  personalization is 0.0–1.0.  Popularity is
// normalized against the result set's max (floored to 100K so a single
// modestly-popular result doesn't get an inflated score).
func blendedScoreFull(
	relevance float64,
	listenCount, maxListenCount int,
	personalization float64,
) float64 {
	effectiveMax := max(maxListenCount, 100_000) //nolint:mnd

	logPop := logNormalize(listenCount, effectiveMax)

	return relevanceWeight*relevance + popularityWeight*logPop + personalizationWeight*personalization
}

// logNormalize maps a count to [0,1] on a log10 scale relative to a
// reference maximum: log10(n+1) / log10(ref+1), clamped.  Shared by the
// blended rerank (dynamic per-result-set ref) and normLog (fixed ref).
func logNormalize(n, ref int) float64 {
	if n <= 0 || ref <= 0 {
		return 0
	}

	v := math.Log10(float64(n)+1) / math.Log10(float64(ref)+1)
	if v > 1.0 {
		return 1.0
	}

	return v
}

// ---------------------------------------------------------------------------
// Lucene query building
// ---------------------------------------------------------------------------
