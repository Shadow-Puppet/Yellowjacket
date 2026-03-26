package explore

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"yellowjacket/backend/database"
)

// Index build parameters.
const (
	// indexTier1Interval is the minimum time between Tier 1
	// (sitewide top lists) refreshes.  Cheap — 12 API calls.
	indexTier1Interval = 7 * 24 * time.Hour

	// indexTier2Interval is the minimum time between Tier 2/4
	// (discography) refreshes.  Incremental — only new artists.
	indexTier2Interval = 30 * 24 * time.Hour

	// indexTopArtists is the number of artists to fetch per range
	// from the LB sitewide endpoint.
	indexTopArtists = 1000

	// indexMaxRGs is the ceiling for release groups per artist.
	indexMaxRGs = 20

	// indexMinRGs is the floor for release groups per artist.
	indexMinRGs = 5

	// indexMaxRecs is the ceiling for recordings per artist.
	indexMaxRecs = 100

	// indexMinRecs is the floor for recordings per artist.
	indexMinRecs = 10

	// indexMinPopularity is the minimum listen count for an entry
	// to be indexed.  Cuts noise from long-tail entries.
	indexMinPopularity = 50

	// indexBatchSize is the number of rows per INSERT transaction.
	indexBatchSize = 100

	// indexerRate is the requests-per-second for the background
	// indexer's dedicated rate limiter (LB allows 30/10s).
	indexerRate = 3

	// indexProgressInterval is how often to log progress.
	indexProgressInterval = 100

	// indexSimilarPerArtist is how many similar artists to consider
	// per library artist for Tier 4 expansion.
	indexSimilarPerArtist = 50

	// indexPopularityExponent controls how steeply the per-artist
	// budget scales with popularity.  Lower = steeper curve.
	// 0.3 means an artist with 1/10th the listens of the max gets
	// ~50% of the budget, not 10%.
	indexPopularityExponent = 0.3

	// labsBaseURL is the base URL for the ListenBrainz labs API.
	labsBaseURL = "https://labs.api.listenbrainz.org"

	// labsSimilarAlgorithm is the algorithm parameter for the
	// similar-artists endpoint.
	labsSimilarAlgorithm = "session_based_days_7500_session_300_contribution_5_threshold_10_limit_100_filter_True_skip_30"
)

// SearchIndexResult is a single hit from the local popularity index.
type SearchIndexResult struct {
	EntityType string `json:"entityType"`
	MBID       string `json:"mbid"`
	Title      string `json:"title"`
	ArtistName string `json:"artistName"`
	ArtistMBID string `json:"artistMbid"`
	Popularity int    `json:"popularity"`
	ExtraJSON  string `json:"extraJson,omitempty"`
}

// lbSitewideArtist is the response shape from the LB sitewide
// top-artists endpoint.
type lbSitewideArtist struct {
	ArtistMBID  string `json:"artist_mbid"`
	ArtistName  string `json:"artist_name"`
	ListenCount int    `json:"listen_count"`
}

// SearchIndex maintains a local SQLite FTS5 index of popular
// albums and tracks from ListenBrainz.  The index is built in the
// background on startup across multiple tiers:
//
//   - Tier 1: sitewide top lists (instant, <5s)
//   - Tier 2: sitewide artists' full discographies (background, ~16min)
//   - Tier 3: library artists' full discographies (background, ~4min)
//   - Tier 4: similar artists to library artists (background, ~24min)
//   - Tier 5: organic growth from user browsing (ongoing, free)
type SearchIndex struct {
	db        *database.DB
	lb        *ListenBrainzClient
	artistImg *ArtistImageProvider
	logger    *slog.Logger

	cancel context.CancelFunc
	done   chan struct{}

	mu         sync.RWMutex
	ready      bool
	maxListens int // highest artist listen count seen, for scaling
}

// NewSearchIndex creates a search index backed by the given
// database.  Call StartBuild to kick off the background populate.
func NewSearchIndex(
	db *database.DB,
	lb *ListenBrainzClient,
	artistImg *ArtistImageProvider,
	logger *slog.Logger,
) *SearchIndex {
	return &SearchIndex{
		db:        db,
		lb:        lb,
		artistImg: artistImg,
		logger:    logger,
		done:      make(chan struct{}),
	}
}

// StartBuild launches the background index build goroutine.
// Returns immediately.
func (si *SearchIndex) StartBuild(ctx context.Context) {
	buildCtx, cancel := context.WithCancel(ctx)
	si.cancel = cancel

	go func() {
		defer close(si.done)

		si.build(buildCtx)
	}()
}

// StopBuild cancels an in-flight build and waits for it to finish.
func (si *SearchIndex) StopBuild() {
	if si.cancel != nil {
		si.cancel()
	}

	<-si.done
}

// IsReady returns true once the index has been built at least once.
func (si *SearchIndex) IsReady() bool {
	si.mu.RLock()
	defer si.mu.RUnlock()

	return si.ready
}

// AddFromCache inserts entries from a cached discography browse
// into the search index (Tier 5: organic growth).  Called when a
// user views an artist page and the discography is fetched.
func (si *SearchIndex) AddFromCache(artistName, artistMBID string, rgs []MBReleaseGroup) {
	if len(rgs) == 0 {
		return
	}

	entries := make([]SearchIndexResult, 0, len(rgs)+1)

	// Add the artist itself.
	entries = append(entries, SearchIndexResult{
		EntityType: "artist",
		MBID:       artistMBID,
		Title:      artistName,
		ArtistName: artistName,
		ArtistMBID: artistMBID,
		Popularity: 0, // Unknown from this path.
	})

	for _, rg := range rgs {
		extra, _ := json.Marshal(map[string]string{"type": rg.PrimaryType})

		entries = append(entries, SearchIndexResult{
			EntityType: "release_group",
			MBID:       rg.MBID,
			Title:      rg.Title,
			ArtistName: artistName,
			ArtistMBID: artistMBID,
			Popularity: 0,
			ExtraJSON:  string(extra),
		})
	}

	si.writeBatch(entries)

	si.logger.Debug("search index: organic add",
		"artist", artistName,
		"releaseGroups", len(rgs),
	)
}

// Search queries the local FTS5 index and returns matches sorted
// by popularity descending.
func (si *SearchIndex) Search(query string, limit int) []SearchIndexResult {
	if !si.IsReady() {
		return nil
	}

	if limit <= 0 {
		limit = 20
	}

	ftsQuery := buildFTSQuery(query)
	if ftsQuery == "" {
		return nil
	}

	rows, err := si.db.QueryContext(`
		SELECT i.entity_type, i.mbid, i.title, i.artist_name,
		       i.artist_mbid, i.popularity, i.extra_json
		FROM explore_index i
		JOIN explore_index_fts f ON f.rowid = i.id
		WHERE explore_index_fts MATCH ?
		ORDER BY i.popularity DESC
		LIMIT ?
	`, ftsQuery, limit)
	if err != nil {
		si.logger.Warn("search index query error",
			"query", query,
			"ftsQuery", ftsQuery,
			"error", err,
		)

		return nil
	}

	defer func() { _ = rows.Close() }()

	var results []SearchIndexResult

	for rows.Next() {
		var r SearchIndexResult

		var extraJSON *string

		if err := rows.Scan(
			&r.EntityType, &r.MBID, &r.Title, &r.ArtistName,
			&r.ArtistMBID, &r.Popularity, &extraJSON,
		); err != nil {
			si.logger.Warn("search index scan error", "error", err)

			continue
		}

		if extraJSON != nil {
			r.ExtraJSON = *extraJSON
		}

		results = append(results, r)
	}

	return results
}

// ---------------------------------------------------------------------------
// FTS query building
// ---------------------------------------------------------------------------

func buildFTSQuery(query string) string {
	words := splitWords(query)
	if len(words) == 0 {
		return ""
	}

	var b strings.Builder

	for i, w := range words {
		if i > 0 {
			b.WriteByte(' ')
		}

		b.WriteString(w)
		b.WriteByte('*')
	}

	return b.String()
}

func splitWords(s string) []string {
	var words []string

	current := ""

	for _, r := range s {
		if isWordChar(r) {
			current += string(r)
		} else if current != "" {
			words = append(words, current)
			current = ""
		}
	}

	if current != "" {
		words = append(words, current)
	}

	return words
}

func isWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r >= 0x80
}

// ---------------------------------------------------------------------------
// Background build — orchestrator
// ---------------------------------------------------------------------------

func (si *SearchIndex) build(ctx context.Context) {
	start := time.Now()

	si.logger.Info("search index build starting")

	// Mark ready from existing rows so search works during the build.
	si.markReadyIfPopulated()

	indexLimiter := NewRateLimiterN(indexerRate)
	indexLB := NewListenBrainzClient(indexLimiter, si.lb.cache, si.logger.WithGroup("indexer"))

	// Tier 1: sitewide instant — refresh weekly (12 calls, <5s).
	tier1Fresh := si.isMetaFresh("tier1_built", indexTier1Interval)

	var sitewideArtists []lbSitewideArtist

	if tier1Fresh {
		si.logger.Info("search index: Tier 1 fresh, loading cached artists")

		sitewideArtists = si.loadCachedSitewideArtists()
	} else {
		sitewideArtists = si.buildTier1Sitewide(ctx, indexLB)

		if ctx.Err() != nil {
			return
		}

		si.setMeta("tier1_built", time.Now().UTC().Format(time.RFC3339))
	}

	si.mu.Lock()
	si.ready = true
	si.mu.Unlock()

	si.logger.Info("search index: Tier 1 complete (sitewide instant)")

	// Tiers 2-4: discographies — refresh monthly, incremental.
	// Only fetch discographies for artists not already indexed.
	discogFresh := si.isMetaFresh("discog_built", indexTier2Interval)

	if discogFresh {
		si.logger.Info("search index: discographies fresh, skipping Tiers 2-4")
	} else {
		indexed := si.indexedArtistMBIDs()

		// Tier 2: sitewide artists' discographies (incremental).
		newSitewide := filterUnindexed(sitewideArtists, indexed)

		si.logger.Info("search index: Tier 2 starting",
			"total", len(sitewideArtists),
			"alreadyIndexed", len(sitewideArtists)-len(newSitewide),
			"new", len(newSitewide),
		)

		si.indexArtistDiscographies(ctx, indexLB, newSitewide, "Tier 2")

		if ctx.Err() != nil {
			return
		}

		si.logger.Info("search index: Tier 2 complete (sitewide discographies)")

		// Tier 3: library artists' discographies (incremental).
		// Re-read indexed set since Tier 2 added entries.
		indexed = si.indexedArtistMBIDs()
		libraryMBIDs := si.buildTier3Library(ctx, indexLB, sitewideArtists, indexed)

		if ctx.Err() != nil {
			return
		}

		si.logger.Info("search index: Tier 3 complete (library discographies)")

		// Tier 4: similar artists (incremental).
		indexed = si.indexedArtistMBIDs()
		si.buildTier4Similar(ctx, indexLB, libraryMBIDs, indexed)

		if ctx.Err() != nil {
			return
		}

		si.logger.Info("search index: Tier 4 complete (similar artists)")

		si.setMeta("discog_built", time.Now().UTC().Format(time.RFC3339))
	}

	si.logger.Info("search index build complete", "elapsed", time.Since(start).Round(time.Second))
}

// ---------------------------------------------------------------------------
// Tier 1: sitewide instant
// ---------------------------------------------------------------------------

// buildTier1Sitewide fetches top artists, recordings, and release
// groups across all time ranges and inserts them.  Returns the
// deduplicated artist list for Tier 2.
func (si *SearchIndex) buildTier1Sitewide(
	ctx context.Context,
	lb *ListenBrainzClient,
) []lbSitewideArtist {
	ranges := []string{"all_time", "this_year", "this_month", "this_week"}
	artistMap := make(map[string]lbSitewideArtist)

	for _, r := range ranges {
		if ctx.Err() != nil {
			break
		}

		// Artists.
		artists, err := si.fetchSitewideArtists(ctx, r)
		if err != nil {
			si.logger.Warn("search index: sitewide artists failed", "range", r, "error", err)

			continue
		}

		for _, a := range artists {
			if _, exists := artistMap[a.ArtistMBID]; !exists {
				artistMap[a.ArtistMBID] = a
			}
		}

		// Recordings.
		recs := si.fetchSitewideRecordings(ctx, lb, r)
		si.upsertSearchResults(recs)

		// Release groups.
		rgs := si.fetchSitewideReleaseGroups(ctx, lb, r)
		si.upsertSearchResults(rgs)
	}

	// Insert all artists and track max popularity.
	artists := make([]lbSitewideArtist, 0, len(artistMap))

	maxL := 0

	for _, a := range artistMap {
		artists = append(artists, a)

		if a.ListenCount > maxL {
			maxL = a.ListenCount
		}
	}

	si.mu.Lock()
	si.maxListens = maxL
	si.mu.Unlock()

	si.upsertArtists(artists)

	si.logger.Info("search index: Tier 1 indexed",
		"artists", len(artists),
	)

	return artists
}

func (si *SearchIndex) fetchSitewideArtists(
	ctx context.Context, timeRange string,
) ([]lbSitewideArtist, error) {
	url := fmt.Sprintf(
		"%s/1/stats/sitewide/artists?count=%d&range=%s",
		listenBrainzBaseURL, indexTopArtists, timeRange,
	)

	req, err := newLBRequest(ctx, url)
	if err != nil {
		return nil, err
	}

	resp, err := si.lb.http.Do(req)
	if err != nil {
		return nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	var envelope struct {
		Payload struct {
			Artists []lbSitewideArtist `json:"artists"`
		} `json:"payload"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, err
	}

	return envelope.Payload.Artists, nil
}

func (si *SearchIndex) fetchSitewideRecordings(
	ctx context.Context, lb *ListenBrainzClient, timeRange string,
) []SearchIndexResult {
	url := fmt.Sprintf(
		"%s/1/stats/sitewide/recordings?count=%d&range=%s",
		listenBrainzBaseURL, indexTopArtists, timeRange,
	)

	body, err := lb.doGet(ctx, url)
	if err != nil {
		si.logger.Warn("search index: sitewide recordings failed",
			"range", timeRange, "error", err,
		)

		return nil
	}

	var envelope struct {
		Payload struct {
			Recordings []struct {
				RecordingMBID string   `json:"recording_mbid"`
				TrackName     string   `json:"track_name"`
				ArtistName    string   `json:"artist_name"`
				ArtistMBIDs   []string `json:"artist_mbids"`
				ListenCount   int      `json:"listen_count"`
			} `json:"recordings"`
		} `json:"payload"`
	}

	if err := json.Unmarshal(body, &envelope); err != nil {
		si.logger.Warn("search index: sitewide recordings unmarshal",
			"range", timeRange, "error", err,
		)

		return nil
	}

	var results []SearchIndexResult

	for _, r := range envelope.Payload.Recordings {
		if r.ListenCount < indexMinPopularity {
			continue
		}

		artistMBID := ""
		if len(r.ArtistMBIDs) > 0 {
			artistMBID = r.ArtistMBIDs[0]
		}

		results = append(results, SearchIndexResult{
			EntityType: "recording",
			MBID:       r.RecordingMBID,
			Title:      r.TrackName,
			ArtistName: r.ArtistName,
			ArtistMBID: artistMBID,
			Popularity: r.ListenCount,
		})
	}

	return results
}

func (si *SearchIndex) fetchSitewideReleaseGroups(
	ctx context.Context, lb *ListenBrainzClient, timeRange string,
) []SearchIndexResult {
	url := fmt.Sprintf(
		"%s/1/stats/sitewide/release-groups?count=%d&range=%s",
		listenBrainzBaseURL, indexTopArtists, timeRange,
	)

	body, err := lb.doGet(ctx, url)
	if err != nil {
		si.logger.Warn("search index: sitewide release groups failed",
			"range", timeRange, "error", err,
		)

		return nil
	}

	var envelope struct {
		Payload struct {
			ReleaseGroups []struct {
				ReleaseGroupMBID string   `json:"release_group_mbid"`
				ReleaseGroupName string   `json:"release_group_name"`
				ArtistName       string   `json:"artist_name"`
				ArtistMBIDs      []string `json:"artist_mbids"`
				ListenCount      int      `json:"listen_count"`
			} `json:"release_groups"`
		} `json:"payload"`
	}

	if err := json.Unmarshal(body, &envelope); err != nil {
		si.logger.Warn("search index: sitewide release groups unmarshal",
			"range", timeRange, "error", err,
		)

		return nil
	}

	var results []SearchIndexResult

	for _, r := range envelope.Payload.ReleaseGroups {
		if r.ListenCount < indexMinPopularity {
			continue
		}

		artistMBID := ""
		if len(r.ArtistMBIDs) > 0 {
			artistMBID = r.ArtistMBIDs[0]
		}

		results = append(results, SearchIndexResult{
			EntityType: "release_group",
			MBID:       r.ReleaseGroupMBID,
			Title:      r.ReleaseGroupName,
			ArtistName: r.ArtistName,
			ArtistMBID: artistMBID,
			Popularity: r.ListenCount,
		})
	}

	return results
}

// ---------------------------------------------------------------------------
// Tier 2: sitewide artists' full discographies
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Tier 3: library artists' full discographies
// ---------------------------------------------------------------------------

// buildTier3Library matches local library artist names against
// sitewide artists by name to get MBIDs, then indexes their
// discographies.  Returns the resolved MBIDs for Tier 4.
func (si *SearchIndex) buildTier3Library(
	ctx context.Context,
	lb *ListenBrainzClient,
	sitewideArtists []lbSitewideArtist,
	indexed map[string]bool,
) []string {
	// Build a name→artist map from sitewide (lowercased).
	nameMap := make(map[string]lbSitewideArtist, len(sitewideArtists))
	for _, a := range sitewideArtists {
		nameMap[strings.ToLower(a.ArtistName)] = a
	}

	// Also build from existing index entries (catches organic adds).
	rows, err := si.db.QueryContext(`
		SELECT DISTINCT artist_name, artist_mbid
		FROM explore_index
		WHERE entity_type = 'artist' AND artist_mbid != ''
	`)
	if err == nil {
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var name, mbid string
			if err := rows.Scan(&name, &mbid); err == nil {
				lower := strings.ToLower(name)
				if _, exists := nameMap[lower]; !exists {
					nameMap[lower] = lbSitewideArtist{
						ArtistMBID: mbid,
						ArtistName: name,
					}
				}
			}
		}
	}

	// Read local library artist names.
	libRows, err := si.db.QueryContext("SELECT DISTINCT name FROM artists")
	if err != nil {
		si.logger.Warn("search index: library artists query failed", "error", err)

		return nil
	}

	defer func() { _ = libRows.Close() }()

	// Collect MBIDs for matched library artists.
	var matched []lbSitewideArtist

	var resolvedMBIDs []string

	for libRows.Next() {
		var name string
		if err := libRows.Scan(&name); err != nil {
			continue
		}

		// Normalize: strip "feat." suffixes.
		normalized := strings.ToLower(name)
		if idx := strings.Index(normalized, " feat."); idx >= 0 {
			normalized = normalized[:idx]
		}

		if idx := strings.Index(normalized, " ft."); idx >= 0 {
			normalized = normalized[:idx]
		}

		normalized = strings.TrimSpace(normalized)

		if a, ok := nameMap[normalized]; ok {
			resolvedMBIDs = append(resolvedMBIDs, a.ArtistMBID)

			// Only index if not already in the index from Tier 2.
			if !indexed[a.ArtistMBID] {
				matched = append(matched, a)
			}
		}
	}

	if len(matched) > 0 {
		si.indexArtistDiscographies(ctx, lb, matched, "Tier 3")
	}

	si.logger.Info("search index: Tier 3 matched",
		"libraryArtists", len(resolvedMBIDs),
		"newToIndex", len(matched),
	)

	return resolvedMBIDs
}

// ---------------------------------------------------------------------------
// Tier 4: similar artists to library artists
// ---------------------------------------------------------------------------

func (si *SearchIndex) buildTier4Similar(
	ctx context.Context,
	lb *ListenBrainzClient,
	libraryMBIDs []string,
	indexed map[string]bool,
) {
	if len(libraryMBIDs) == 0 {
		return
	}

	// Fetch similar artists for each library artist.
	newArtistMap := make(map[string]lbSitewideArtist)

	var mu sync.Mutex

	sem := make(chan struct{}, indexerRate)

	var wg sync.WaitGroup

	var completed atomic.Int32

	for _, mbid := range libraryMBIDs {
		if ctx.Err() != nil {
			break
		}

		sem <- struct{}{}

		wg.Add(1)

		go func(artistMBID string) {
			defer func() {
				<-sem
				wg.Done()
			}()

			similar := si.fetchSimilarArtists(ctx, artistMBID)

			mu.Lock()

			for _, s := range similar {
				if !indexed[s.ArtistMBID] {
					if _, exists := newArtistMap[s.ArtistMBID]; !exists {
						newArtistMap[s.ArtistMBID] = lbSitewideArtist{
							ArtistMBID: s.ArtistMBID,
							ArtistName: s.Name,
						}
					}
				}
			}

			mu.Unlock()

			n := completed.Add(1)
			if int(n)%indexProgressInterval == 0 {
				si.logger.Info("search index: Tier 4 similar progress",
					"completed", n,
					"total", len(libraryMBIDs),
				)
			}
		}(mbid)
	}

	wg.Wait()

	if len(newArtistMap) == 0 {
		return
	}

	newArtists := make([]lbSitewideArtist, 0, len(newArtistMap))
	for _, a := range newArtistMap {
		newArtists = append(newArtists, a)
	}

	si.logger.Info("search index: Tier 4 discovered",
		"newArtists", len(newArtists),
	)

	si.indexArtistDiscographies(ctx, lb, newArtists, "Tier 4")
}

type lbSimilarArtistWire struct {
	ArtistMBID string `json:"artist_mbid"`
	Name       string `json:"name"`
	Score      int    `json:"score"`
}

func (si *SearchIndex) fetchSimilarArtists(
	ctx context.Context, artistMBID string,
) []lbSimilarArtistWire {
	url := fmt.Sprintf(
		"%s/similar-artists/json?artist_mbids=%s&algorithm=%s",
		labsBaseURL, artistMBID, labsSimilarAlgorithm,
	)

	req, err := newLBRequest(ctx, url)
	if err != nil {
		return nil
	}

	resp, err := si.lb.http.Do(req)
	if err != nil {
		return nil
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var results []lbSimilarArtistWire
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil
	}

	limit := indexSimilarPerArtist
	if limit > len(results) {
		limit = len(results)
	}

	return results[:limit]
}

// ---------------------------------------------------------------------------
// Shared: index artist discographies
// ---------------------------------------------------------------------------

// indexArtistDiscographies fetches top release groups and recordings
// for each artist and inserts them into the index.  Used by Tiers 2-4.
func (si *SearchIndex) indexArtistDiscographies(
	ctx context.Context,
	lb *ListenBrainzClient,
	artists []lbSitewideArtist,
	tier string,
) {
	if len(artists) == 0 {
		return
	}

	sem := make(chan struct{}, indexerRate)

	var wg sync.WaitGroup

	var completed atomic.Int32

	for _, a := range artists {
		if ctx.Err() != nil {
			break
		}

		sem <- struct{}{}

		wg.Add(1)

		go func(artist lbSitewideArtist) {
			defer func() {
				<-sem
				wg.Done()
			}()

			si.indexOneArtist(ctx, lb, artist)

			n := completed.Add(1)
			if int(n)%indexProgressInterval == 0 {
				si.logger.Info("search index progress",
					"tier", tier,
					"completed", n,
					"total", len(artists),
					"pct", fmt.Sprintf("%.0f%%", float64(n)/float64(len(artists))*100),
				)
			}
		}(a)
	}

	wg.Wait()

	si.logger.Info("search index: discographies indexed",
		"tier", tier,
		"artists", len(artists),
	)
}

func (si *SearchIndex) indexOneArtist(
	ctx context.Context,
	lb *ListenBrainzClient,
	artist lbSitewideArtist,
) {
	if ctx.Err() != nil {
		return
	}

	rgLimit, recLimit := si.scaledLimits(artist.ListenCount)

	// Run LB discography fetches and MB artist image resolution
	// concurrently — they use different rate limiters so they
	// don't block each other.
	var (
		rgs  []SearchIndexResult
		recs []SearchIndexResult
		wg   sync.WaitGroup
	)

	// LB pipeline: top release groups + top recordings.
	wg.Add(1)

	go func() {
		defer wg.Done()

		rgs = si.fetchTopReleaseGroups(ctx, lb, artist, rgLimit)
		recs = si.fetchTopRecordings(ctx, lb, artist, recLimit)
	}()

	// MB pipeline: resolve + cache artist image (uses MB rate limiter).
	wg.Add(1)

	go func() {
		defer wg.Done()

		if si.artistImg != nil {
			si.artistImg.GetArtistImage(artist.ArtistMBID)
		}
	}()

	wg.Wait()

	// Batch write discography results.
	all := make([]SearchIndexResult, 0, len(rgs)+len(recs))
	all = append(all, rgs...)
	all = append(all, recs...)

	for i := 0; i < len(all); i += indexBatchSize {
		end := i + indexBatchSize
		if end > len(all) {
			end = len(all)
		}

		si.writeBatch(all[i:end])
	}
}

func (si *SearchIndex) fetchTopReleaseGroups(
	ctx context.Context,
	lb *ListenBrainzClient,
	artist lbSitewideArtist,
	maxCount int,
) []SearchIndexResult {
	url := fmt.Sprintf(
		"%s/1/popularity/top-release-groups-for-artist/%s",
		listenBrainzBaseURL, artist.ArtistMBID,
	)

	body, err := lb.doGet(ctx, url)
	if err != nil {
		si.logger.Debug("search index: top RGs failed",
			"artist", artist.ArtistName,
			"error", err,
		)

		return nil
	}

	var raw []struct {
		ReleaseGroupMBID    string `json:"release_group_mbid"`
		TotalListenCount    int    `json:"total_listen_count"`
		CAAId               *int64 `json:"caa_id"`
		CAAReleaseGroupMBID string `json:"caa_release_mbid"`
		ReleaseGroup        struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"release_group"`
		Artist struct {
			Artists []struct {
				ArtistMBID string `json:"artist_mbid"`
				Name       string `json:"name"`
			} `json:"artists"`
		} `json:"artist"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil
	}

	limit := maxCount
	if limit > len(raw) {
		limit = len(raw)
	}

	results := make([]SearchIndexResult, 0, limit)

	for _, r := range raw[:limit] {
		if r.TotalListenCount < indexMinPopularity {
			continue
		}

		artistName := artist.ArtistName
		artistMBID := artist.ArtistMBID

		if len(r.Artist.Artists) > 0 {
			artistName = r.Artist.Artists[0].Name
			artistMBID = r.Artist.Artists[0].ArtistMBID
		}

		extraMap := map[string]any{"type": r.ReleaseGroup.Type}
		if r.CAAId != nil {
			extraMap["caaId"] = *r.CAAId
		}

		if r.CAAReleaseGroupMBID != "" {
			extraMap["caaReleaseMbid"] = r.CAAReleaseGroupMBID
		}

		extra, _ := json.Marshal(extraMap)

		results = append(results, SearchIndexResult{
			EntityType: "release_group",
			MBID:       r.ReleaseGroupMBID,
			Title:      r.ReleaseGroup.Name,
			ArtistName: artistName,
			ArtistMBID: artistMBID,
			Popularity: r.TotalListenCount,
			ExtraJSON:  string(extra),
		})
	}

	return results
}

func (si *SearchIndex) fetchTopRecordings(
	ctx context.Context,
	lb *ListenBrainzClient,
	artist lbSitewideArtist,
	maxCount int,
) []SearchIndexResult {
	url := fmt.Sprintf(
		"%s/1/popularity/top-recordings-for-artist/%s",
		listenBrainzBaseURL, artist.ArtistMBID,
	)

	body, err := lb.doGet(ctx, url)
	if err != nil {
		return nil
	}

	var raw []lbTopRecordingWire
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil
	}

	limit := maxCount
	if limit > len(raw) {
		limit = len(raw)
	}

	results := make([]SearchIndexResult, 0, limit)

	for _, r := range raw[:limit] {
		if r.TotalListenCount < indexMinPopularity {
			continue
		}

		results = append(results, SearchIndexResult{
			EntityType: "recording",
			MBID:       r.RecordingMBID,
			Title:      r.RecordingName,
			ArtistName: r.ArtistName,
			ArtistMBID: artist.ArtistMBID,
			Popularity: r.TotalListenCount,
		})
	}

	return results
}

// ---------------------------------------------------------------------------
// Database writes
// ---------------------------------------------------------------------------

func (si *SearchIndex) upsertArtists(artists []lbSitewideArtist) {
	batch := make([]SearchIndexResult, 0, indexBatchSize)

	for _, a := range artists {
		batch = append(batch, SearchIndexResult{
			EntityType: "artist",
			MBID:       a.ArtistMBID,
			Title:      a.ArtistName,
			ArtistName: a.ArtistName,
			ArtistMBID: a.ArtistMBID,
			Popularity: a.ListenCount,
		})

		if len(batch) >= indexBatchSize {
			si.writeBatch(batch)
			batch = batch[:0]
		}
	}

	if len(batch) > 0 {
		si.writeBatch(batch)
	}
}

func (si *SearchIndex) upsertSearchResults(results []SearchIndexResult) {
	for i := 0; i < len(results); i += indexBatchSize {
		end := i + indexBatchSize
		if end > len(results) {
			end = len(results)
		}

		si.writeBatch(results[i:end])
	}
}

func (si *SearchIndex) writeBatch(entries []SearchIndexResult) {
	if len(entries) == 0 {
		return
	}

	tx, err := si.db.BeginTx()
	if err != nil {
		si.logger.Warn("search index: begin tx error", "error", err)

		return
	}

	for _, e := range entries {
		if _, err := tx.Exec(`
			INSERT OR REPLACE INTO explore_index
				(entity_type, mbid, title, artist_name, artist_mbid, popularity, extra_json)
			VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, ''))
		`, e.EntityType, e.MBID, e.Title, e.ArtistName, e.ArtistMBID, e.Popularity, e.ExtraJSON,
		); err != nil {
			si.logger.Warn("search index: insert error",
				"mbid", e.MBID,
				"error", err,
			)
		}
	}

	if err := tx.Commit(); err != nil {
		si.logger.Warn("search index: commit error", "error", err)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (si *SearchIndex) indexedArtistMBIDs() map[string]bool {
	rows, err := si.db.QueryContext(
		"SELECT DISTINCT artist_mbid FROM explore_index WHERE entity_type = 'artist'",
	)
	if err != nil {
		return nil
	}

	defer func() { _ = rows.Close() }()

	result := make(map[string]bool)

	for rows.Next() {
		var mbid string
		if err := rows.Scan(&mbid); err == nil {
			result[mbid] = true
		}
	}

	return result
}

func (si *SearchIndex) isMetaFresh(key string, maxAge time.Duration) bool {
	rows, err := si.db.QueryContext(
		"SELECT value FROM explore_index_meta WHERE key = ?", key,
	)
	if err != nil {
		return false
	}

	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return false
	}

	var val string
	if err := rows.Scan(&val); err != nil {
		return false
	}

	t, err := time.Parse(time.RFC3339, val)
	if err != nil {
		return false
	}

	return time.Since(t) < maxAge
}

// loadCachedSitewideArtists reads artist entries from the existing
// index when Tier 1 is fresh and doesn't need re-fetching.
func (si *SearchIndex) loadCachedSitewideArtists() []lbSitewideArtist {
	rows, err := si.db.QueryContext(`
		SELECT mbid, title, popularity
		FROM explore_index
		WHERE entity_type = 'artist'
		ORDER BY popularity DESC
	`)
	if err != nil {
		return nil
	}

	defer func() { _ = rows.Close() }()

	var artists []lbSitewideArtist

	maxL := 0

	for rows.Next() {
		var a lbSitewideArtist
		if err := rows.Scan(&a.ArtistMBID, &a.ArtistName, &a.ListenCount); err == nil {
			artists = append(artists, a)

			if a.ListenCount > maxL {
				maxL = a.ListenCount
			}
		}
	}

	si.mu.Lock()
	si.maxListens = maxL
	si.mu.Unlock()

	return artists
}

// filterUnindexed returns artists whose MBIDs are not in the
// indexed set.
func filterUnindexed(artists []lbSitewideArtist, indexed map[string]bool) []lbSitewideArtist {
	var out []lbSitewideArtist

	for _, a := range artists {
		if !indexed[a.ArtistMBID] {
			out = append(out, a)
		}
	}

	return out
}

func (si *SearchIndex) setMeta(key, value string) {
	if _, err := si.db.ExecContext(
		"INSERT OR REPLACE INTO explore_index_meta (key, value) VALUES (?, ?)",
		key, value,
	); err != nil {
		si.logger.Warn("search index: set meta error", "key", key, "error", err)
	}
}

func (si *SearchIndex) markReadyIfPopulated() {
	rows, err := si.db.QueryContext("SELECT COUNT(*) FROM explore_index")
	if err != nil {
		return
	}

	defer func() { _ = rows.Close() }()

	if rows.Next() {
		var count int
		if err := rows.Scan(&count); err == nil && count > 0 {
			si.mu.Lock()
			si.ready = true
			si.mu.Unlock()

			si.logger.Info("search index: using existing index", "entries", count)
		}
	}
}

// scaledLimits returns the number of release groups and recordings
// to index for an artist with the given listen count, scaled by
// popularity relative to the most popular artist in the index.
func (si *SearchIndex) scaledLimits(listenCount int) (rgs, recs int) {
	si.mu.RLock()
	maxL := si.maxListens
	si.mu.RUnlock()

	if maxL <= 0 || listenCount <= 0 {
		return indexMinRGs, indexMinRecs
	}

	ratio := math.Pow(float64(listenCount)/float64(maxL), indexPopularityExponent)

	rgs = int(float64(indexMinRGs) + ratio*float64(indexMaxRGs-indexMinRGs))
	recs = int(float64(indexMinRecs) + ratio*float64(indexMaxRecs-indexMinRecs))

	rgs = max(indexMinRGs, min(indexMaxRGs, rgs))
	recs = max(indexMinRecs, min(indexMaxRecs, recs))

	return rgs, recs
}

// newLBRequest creates an HTTP GET request with the LB User-Agent.
func newLBRequest(ctx context.Context, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", lbUserAgent)

	return req, nil
}
