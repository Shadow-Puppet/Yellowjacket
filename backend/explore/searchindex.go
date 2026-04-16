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
	"yellowjacket/backend/events"

	"github.com/wailsapp/wails/v2/pkg/runtime"
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
	// Top-popularity artists get their full discography.
	indexMaxRGs = 50

	// indexMinRGs is the floor for release groups per artist.
	// Even the least popular indexed artist gets a couple of albums.
	indexMinRGs = 2

	// indexMaxRecs is the ceiling for recordings per artist.
	indexMaxRecs = 200

	// indexMinRecs is the floor for recordings per artist.
	indexMinRecs = 5

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

	// indexSimilarPerArtist is how many similar artists to store
	// per library artist in similar_artist_map and to consider
	// for Tier 4 discography expansion.
	indexSimilarPerArtist = 20

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

	// similarArtistsBatchSize is the number of seed MBIDs processed
	// in one logging "batch" during Tier 4.  The labs multi-seed
	// POST form is broken, so we actually issue one GET per seed
	// (concurrency bounded by indexerRate); batching here just
	// keeps progress log output bounded.
	similarArtistsBatchSize = 50
)

// SearchIndexResult is a single hit from the local popularity index.
type SearchIndexResult struct {
	EntityType    string `json:"entityType"`
	MBID          string `json:"mbid"`
	Title         string `json:"title"`
	ArtistName    string `json:"artistName"`
	ArtistMBID    string `json:"artistMbid"`
	Aliases       string `json:"aliases,omitempty"`

	// Popularity signals.
	Popularity    int `json:"popularity"`
	ListenerCount int `json:"listenerCount"`

	// Recording-specific fields.
	Duration       int    `json:"duration"` // milliseconds
	CAAReleaseMBID string `json:"caaReleaseMbid"`
	ReleaseName    string `json:"releaseName"`

	// Release-group-specific fields.
	PrimaryType    string `json:"primaryType"`
	SecondaryTypes string `json:"secondaryTypes"` // comma-separated
	ReleaseDate    string `json:"releaseDate"`

	// Artist-specific fields (from MB lookup).
	ArtistType     string `json:"artistType"`
	Country        string `json:"country"`
	Disambiguation string `json:"disambiguation"`
	SortName       string `json:"sortName"`

	// Personalization.
	InLibrary bool `json:"inLibrary"`
	IsSimilar bool `json:"isSimilar"`

	// DiscogFetched marks an artist row as having had its full
	// discography (release groups + recordings) fetched by the
	// indexer pipeline.  Only set on artist entity_type entries.
	// Used by indexedArtistMBIDs() to skip already-processed
	// artists in tier 2/3.
	DiscogFetched bool `json:"-"`

	// Local library cross-reference (0 if not owned).
	LocalArtistID       int64 `json:"localArtistId,omitempty"`
	LocalReleaseGroupID int64 `json:"localReleaseGroupId,omitempty"`
	LocalRecordingID    int64 `json:"localRecordingId,omitempty"`

	// Schema version for staleness detection.
	SchemaVersion int `json:"-"`
}

// currentSchemaVersion is bumped when we add new fields that should
// trigger re-indexing of existing rows.  The build logic checks each
// artist's rows against this version and re-fetches if stale.
const currentSchemaVersion = 1

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
	runtimeCtx context.Context // Wails runtime context for event emission

	cancel context.CancelFunc
	done   chan struct{}

	mu         sync.RWMutex
	ready      bool
	maxListens int // highest artist listen count seen, for scaling

	// Build status tracking — read by GetIndexStatus for the UI.
	buildStatus IndexStatus
}

// TierStatus represents the state of a single index tier.
type TierStatus struct {
	Name      string `json:"name"`
	State     string `json:"state"` // "pending", "running", "complete", "error", "skipped"
	Total     int    `json:"total"`
	Completed int    `json:"completed"`
	Error     string `json:"error,omitempty"`
}

// IndexStatus is the full index build status, exposed to the frontend.
type IndexStatus struct {
	Building      bool         `json:"building"`
	Ready         bool         `json:"ready"`
	LastBuilt     string       `json:"lastBuilt,omitempty"` // RFC3339 timestamp of last complete build
	Tiers         []TierStatus `json:"tiers"`
	Artists       int          `json:"artists"`
	Recordings    int          `json:"recordings"`
	ReleaseGroups int          `json:"releaseGroups"`
	TotalRows     int          `json:"totalRows"`
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
	}
}

// SetContext injects the Wails runtime context for event emission.
func (si *SearchIndex) SetContext(ctx context.Context) {
	si.runtimeCtx = ctx

	// Initialize buildStatus with empty (non-nil) tiers so the
	// frontend always receives a valid array, not JSON null.
	si.mu.Lock()
	if si.buildStatus.Tiers == nil {
		si.buildStatus.Tiers = []TierStatus{}
	}
	si.mu.Unlock()

	// Load current row counts + last-built timestamp from DB.
	si.refreshStatusCounts()

	// Start a background ticker that emits status every 3 seconds.
	// This replaces frontend polling — the Wails binding dispatcher
	// can be blocked by other calls, but EventsEmit bypasses it.
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				si.emitStatus()
			}
		}
	}()
}

// IndexNewArtists indexes only library artists that are not yet in the
// search index. This is the lightweight post-scan path — no tier
// machinery, no freshness checks, no sitewide/similar artist logic.
// Just finds library artists with MBIDs missing from the index and
// fetches their discographies + images.
func (si *SearchIndex) IndexNewArtists(ctx context.Context) {
	si.mu.Lock()
	if si.cancel != nil {
		// Full build already running — it will pick up new artists.
		si.mu.Unlock()

		return
	}

	si.done = make(chan struct{})
	si.mu.Unlock()

	buildCtx, cancel := context.WithCancel(ctx)

	si.mu.Lock()
	si.cancel = cancel
	si.mu.Unlock()

	go func() {
		defer func() {
			si.mu.Lock()
			si.cancel = nil
			si.mu.Unlock()

			close(si.done)

			// Mark ready if we indexed anything, so search works
			// while the full tier build is pending.
			si.MarkReadyIfPopulated()
		}()

		si.indexNewLibraryArtists(buildCtx)
	}()
}

// indexNewLibraryArtists finds library artists with MBIDs that are not
// in the index and fetches their discographies.
func (si *SearchIndex) indexNewLibraryArtists(ctx context.Context) {
	indexed := si.indexedArtistMBIDs()
	libraryMBIDs := si.getLibraryArtistMBIDs()

	var newArtists []lbSitewideArtist

	for _, mbid := range libraryMBIDs {
		if !indexed[mbid] {
			// Look up the artist name from the DB.
			var name string

			rows, err := si.db.QueryContext(
				"SELECT name FROM artists WHERE mbid = ? LIMIT 1", mbid,
			)
			if err != nil {
				continue
			}

			if !rows.Next() {
				_ = rows.Close()

				continue
			}

			if err := rows.Scan(&name); err != nil {
				_ = rows.Close()

				continue
			}

			_ = rows.Close()

			newArtists = append(newArtists, lbSitewideArtist{
				ArtistMBID: mbid,
				ArtistName: name,
			})
		}
	}

	if len(newArtists) == 0 {
		si.logger.Info("search index: no new library artists to index")

		return
	}

	si.logger.Info("search index: indexing new library artists",
		"count", len(newArtists),
	)

	indexLimiter := NewRateLimiterN(indexerRate)
	indexLB := NewListenBrainzClient(indexLimiter, si.lb.cache, si.logger.WithGroup("indexer"))

	si.indexArtistDiscographies(ctx, indexLB, newArtists, "new-artists", true)

	si.logger.Info("search index: new library artists indexed",
		"count", len(newArtists),
	)
}

// StartBuild launches the background index build goroutine.
// Returns immediately.
func (si *SearchIndex) StartBuild(ctx context.Context) {
	si.mu.Lock()
	// Don't start if already running.
	if si.cancel != nil {
		si.mu.Unlock()

		return
	}

	si.done = make(chan struct{})
	si.mu.Unlock()

	buildCtx, cancel := context.WithCancel(ctx)

	si.mu.Lock()
	si.cancel = cancel
	si.mu.Unlock()

	go func() {
		defer func() {
			si.mu.Lock()
			si.cancel = nil
			si.mu.Unlock()

			close(si.done)
		}()

		si.build(buildCtx)
	}()
}

// StopBuild cancels an in-flight build and waits for it to finish.
// Safe to call even if no build is running.
func (si *SearchIndex) StopBuild() {
	si.mu.RLock()
	cancel := si.cancel
	done := si.done
	si.mu.RUnlock()

	if cancel != nil {
		cancel()
	}

	if done != nil {
		<-done
	}
}

// WaitForIdle blocks until no build or indexing goroutine is running.
// Unlike StopBuild, this does NOT cancel a running build.
func (si *SearchIndex) WaitForIdle() {
	si.mu.RLock()
	done := si.done
	si.mu.RUnlock()

	if done != nil {
		<-done
	}
}

// IsReady returns true once the index has been built at least once.
func (si *SearchIndex) IsReady() bool {
	si.mu.RLock()
	defer si.mu.RUnlock()

	return si.ready
}

// GetIndexStatus returns the current index build status for the UI.
// Entirely in-memory — no DB queries — to avoid blocking the Wails
// UI thread when the index build holds a write lock.
func (si *SearchIndex) GetIndexStatus() IndexStatus {
	si.mu.RLock()
	status := si.buildStatus
	status.Ready = si.ready
	status.Building = si.cancel != nil
	si.mu.RUnlock()

	return status
}

// refreshStatusCounts updates the row counts and last-built timestamp
// in buildStatus from the DB.  Called between tiers when the DB is idle.
func (si *SearchIndex) refreshStatusCounts() {
	var artists, recordings, rgs int

	rows, err := si.db.QueryContext(`
		SELECT entity_type, COUNT(*) FROM explore_index GROUP BY entity_type
	`)
	if err == nil {
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var et string
			var count int

			if err := rows.Scan(&et, &count); err == nil {
				switch et {
				case "artist":
					artists = count
				case "recording":
					recordings = count
				case "release_group":
					rgs = count
				}
			}
		}
	}

	var lastBuilt string

	metaRow, err := si.db.QueryContext(
		"SELECT value FROM explore_index_meta WHERE key = 'tier5_built'",
	)
	if err == nil {
		defer func() { _ = metaRow.Close() }()

		if metaRow.Next() {
			_ = metaRow.Scan(&lastBuilt)
		}
	}

	si.mu.Lock()
	si.buildStatus.Artists = artists
	si.buildStatus.Recordings = recordings
	si.buildStatus.ReleaseGroups = rgs
	si.buildStatus.TotalRows = artists + recordings + rgs
	si.buildStatus.LastBuilt = lastBuilt
	si.mu.Unlock()

	si.emitStatus()
}

// setTierStatus updates the build status for a named tier.
func (si *SearchIndex) setTierStatus(name, state string, total, completed int) {
	si.mu.Lock()

	for i := range si.buildStatus.Tiers {
		if si.buildStatus.Tiers[i].Name == name {
			si.buildStatus.Tiers[i].State = state
			si.buildStatus.Tiers[i].Total = total
			si.buildStatus.Tiers[i].Completed = completed
			si.mu.Unlock()
			si.emitStatus()

			return
		}
	}

	si.buildStatus.Tiers = append(si.buildStatus.Tiers, TierStatus{
		Name:      name,
		State:     state,
		Total:     total,
		Completed: completed,
	})

	si.mu.Unlock()
	si.emitStatus()
}

// setTierError marks a tier as errored.
func (si *SearchIndex) setTierError(name, errMsg string) {
	si.mu.Lock()

	for i := range si.buildStatus.Tiers {
		if si.buildStatus.Tiers[i].Name == name {
			si.buildStatus.Tiers[i].State = "error"
			si.buildStatus.Tiers[i].Error = errMsg
			si.mu.Unlock()
			si.emitStatus()

			return
		}
	}

	si.mu.Unlock()
}

// emitStatus pushes the current index status to the frontend via Wails event.
func (si *SearchIndex) emitStatus() {
	if si.runtimeCtx == nil {
		return
	}

	si.mu.RLock()
	status := si.buildStatus
	status.Ready = si.ready
	status.Building = si.cancel != nil
	si.mu.RUnlock()

	si.logger.Info("emitting index status event",
		"building", status.Building,
		"tiers", len(status.Tiers),
		"ready", status.Ready,
	)

	runtime.EventsEmit(si.runtimeCtx, events.IndexStatusChanged, status)
}

// GetPopularity returns the cached popularity (listen count) for
// the given MBID from the local index.  Returns 0 if not found.
func (si *SearchIndex) GetPopularity(mbid string) int {
	if mbid == "" {
		return 0
	}

	rows, err := si.db.QueryContext(
		"SELECT popularity FROM explore_index WHERE mbid = ? LIMIT 1",
		mbid,
	)
	if err != nil {
		return 0
	}

	defer func() { _ = rows.Close() }()

	if rows.Next() {
		var pop int
		if err := rows.Scan(&pop); err == nil {
			return pop
		}
	}

	return 0
}

// PopularityBatchResult contains popularity, listener count, library
// status, and similarity scores for a batch of MBIDs.
type PopularityBatchResult struct {
	Popularity       map[string]int
	ListenerCount    map[string]int
	InLibrary        map[string]bool
	SimilarityScores map[string]int // max similarity score (0 = not similar)
}

// GetPopularityBatch returns popularity (listen count) and library
// status for multiple MBIDs in a single query.
func (si *SearchIndex) GetPopularityBatch(mbids []string) *PopularityBatchResult {
	if len(mbids) == 0 {
		return nil
	}

	placeholders := make([]string, len(mbids))
	args := make([]any, len(mbids))

	for i, m := range mbids {
		placeholders[i] = "?"
		args[i] = m
	}

	query := "SELECT mbid, popularity, listener_count, in_library FROM explore_index WHERE mbid IN (" +
		strings.Join(placeholders, ",") + ")"

	rows, err := si.db.QueryContext(query, args...)
	if err != nil {
		return nil
	}

	defer func() { _ = rows.Close() }()

	result := &PopularityBatchResult{
		Popularity:       make(map[string]int, len(mbids)),
		ListenerCount:    make(map[string]int),
		InLibrary:        make(map[string]bool),
		SimilarityScores: make(map[string]int),
	}

	for rows.Next() {
		var mbid string
		var pop int
		var listeners int
		var inLib int

		if err := rows.Scan(&mbid, &pop, &listeners, &inLib); err == nil {
			existing, ok := result.Popularity[mbid]
			if !ok || pop > existing {
				result.Popularity[mbid] = pop
			}

			if listeners > 0 {
				result.ListenerCount[mbid] = listeners
			}

			if inLib == 1 {
				result.InLibrary[mbid] = true
			}
		}
	}

	// Fetch similarity scores from the map table.
	result.SimilarityScores = si.GetSimilarityScores(mbids)

	return result
}

// IsInLibrary returns whether the given MBID is marked as in the
// user's local library in the search index.
func (si *SearchIndex) IsInLibrary(mbid string) bool {
	if mbid == "" {
		return false
	}

	rows, err := si.db.QueryContext(
		"SELECT in_library FROM explore_index WHERE mbid = ? AND in_library = 1 LIMIT 1",
		mbid,
	)
	if err != nil {
		return false
	}

	defer func() { _ = rows.Close() }()

	return rows.Next()
}

// LookupArtistByMBID reads a single artist row from the index, including
// all metadata fields (type, country, disambiguation, sort_name).
// Returns nil if the artist isn't indexed.
func (si *SearchIndex) LookupArtistByMBID(mbid string) *SearchIndexResult {
	rows, err := si.db.QueryContext(
		`SELECT title, artist_name, artist_mbid, popularity, listener_count,
		        artist_type, country, disambiguation, sort_name, aliases,
		        in_library, is_similar, COALESCE(local_artist_id, 0)
		 FROM explore_index
		 WHERE mbid = ? AND entity_type = 'artist' LIMIT 1`,
		mbid,
	)
	if err != nil {
		return nil
	}

	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return nil
	}

	r := SearchIndexResult{
		EntityType: "artist",
		MBID:       mbid,
	}

	if err := rows.Scan(
		&r.Title, &r.ArtistName, &r.ArtistMBID, &r.Popularity, &r.ListenerCount,
		&r.ArtistType, &r.Country, &r.Disambiguation, &r.SortName, &r.Aliases,
		&r.InLibrary, &r.IsSimilar, &r.LocalArtistID,
	); err != nil {
		return nil
	}

	return &r
}

// ReleaseGroupMBIDsForCAAReleaseMBIDs takes a list of release MBIDs
// (from recording.caa_release_mbid) and returns a map from release
// MBID → release group MBID, by joining against the release_group
// rows whose caa_release_mbid matches.  Used to find parent release
// groups for tracks so we can fetch cover art via the existing
// release-group endpoint instead of the per-release endpoint.
func (si *SearchIndex) ReleaseGroupMBIDsForCAAReleaseMBIDs(caaReleaseMBIDs []string) map[string]string {
	if len(caaReleaseMBIDs) == 0 {
		return nil
	}

	// Filter out empty strings — an empty input MBID would match
	// every release_group row that also has an empty caa_release_mbid,
	// producing false positives that resolve to release groups with
	// no actual cover art (e.g. bootlegs, demos).
	filtered := make([]string, 0, len(caaReleaseMBIDs))
	seen := make(map[string]struct{}, len(caaReleaseMBIDs))

	for _, m := range caaReleaseMBIDs {
		if m == "" {
			continue
		}

		if _, dup := seen[m]; dup {
			continue
		}

		seen[m] = struct{}{}
		filtered = append(filtered, m)
	}

	if len(filtered) == 0 {
		return nil
	}

	placeholders := make([]string, len(filtered))
	args := make([]any, len(filtered))

	for i, m := range filtered {
		placeholders[i] = "?"
		args[i] = m
	}

	query := `SELECT caa_release_mbid, mbid
	          FROM explore_index
	          WHERE entity_type = 'release_group'
	            AND caa_release_mbid != ''
	            AND caa_release_mbid IN (` + strings.Join(placeholders, ",") + `)`

	rows, err := si.db.QueryContext(query, args...)
	if err != nil {
		return nil
	}

	defer func() { _ = rows.Close() }()

	out := make(map[string]string, len(filtered))

	for rows.Next() {
		var caaMBID, rgMBID string
		if err := rows.Scan(&caaMBID, &rgMBID); err == nil {
			out[caaMBID] = rgMBID
		}
	}

	return out
}

// LookupReleaseGroupByMBID reads a single release group row from the index.
func (si *SearchIndex) LookupReleaseGroupByMBID(mbid string) *SearchIndexResult {
	rows, err := si.db.QueryContext(
		`SELECT title, artist_name, artist_mbid, popularity, listener_count,
		        primary_type, secondary_types, release_date,
		        in_library, COALESCE(local_release_group_id, 0)
		 FROM explore_index
		 WHERE mbid = ? AND entity_type = 'release_group' LIMIT 1`,
		mbid,
	)
	if err != nil {
		return nil
	}

	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return nil
	}

	r := SearchIndexResult{
		EntityType: "release_group",
		MBID:       mbid,
	}

	if err := rows.Scan(
		&r.Title, &r.ArtistName, &r.ArtistMBID, &r.Popularity, &r.ListenerCount,
		&r.PrimaryType, &r.SecondaryTypes, &r.ReleaseDate,
		&r.InLibrary, &r.LocalReleaseGroupID,
	); err != nil {
		return nil
	}

	return &r
}

// TopRecordingsByArtist returns the most popular recordings for an
// artist MBID from the index, ordered by popularity descending.
// Falls back to returning entries without popularity data if there
// aren't enough popular ones.
func (si *SearchIndex) TopRecordingsByArtist(artistMBID string, limit int) []SearchIndexResult {
	rows, err := si.db.QueryContext(
		`SELECT mbid, title, artist_name, popularity, listener_count,
		        duration, caa_release_mbid, release_name,
		        in_library, COALESCE(local_recording_id, 0)
		 FROM explore_index
		 WHERE artist_mbid = ? AND entity_type = 'recording'
		 ORDER BY popularity DESC
		 LIMIT ?`,
		artistMBID, limit,
	)
	if err != nil {
		return nil
	}

	defer func() { _ = rows.Close() }()

	var results []SearchIndexResult

	for rows.Next() {
		var r SearchIndexResult
		if err := rows.Scan(
			&r.MBID, &r.Title, &r.ArtistName, &r.Popularity, &r.ListenerCount,
			&r.Duration, &r.CAAReleaseMBID, &r.ReleaseName,
			&r.InLibrary, &r.LocalRecordingID,
		); err == nil {
			r.EntityType = "recording"
			r.ArtistMBID = artistMBID
			results = append(results, r)
		}
	}

	return results
}

// TopReleaseGroupsByArtist returns the most popular release groups
// for an artist MBID from the index, ordered by popularity descending.
// Falls back to returning entries without popularity data if there
// aren't enough popular ones.
func (si *SearchIndex) TopReleaseGroupsByArtist(artistMBID string, limit int) []SearchIndexResult {
	rows, err := si.db.QueryContext(
		`SELECT mbid, title, artist_name, popularity, listener_count,
		        primary_type, secondary_types, release_date,
		        in_library, COALESCE(local_release_group_id, 0)
		 FROM explore_index
		 WHERE artist_mbid = ? AND entity_type = 'release_group'
		 ORDER BY popularity DESC
		 LIMIT ?`,
		artistMBID, limit,
	)
	if err != nil {
		return nil
	}

	defer func() { _ = rows.Close() }()

	var results []SearchIndexResult

	for rows.Next() {
		var r SearchIndexResult
		if err := rows.Scan(
			&r.MBID, &r.Title, &r.ArtistName, &r.Popularity, &r.ListenerCount,
			&r.PrimaryType, &r.SecondaryTypes, &r.ReleaseDate,
			&r.InLibrary, &r.LocalReleaseGroupID,
		); err == nil {
			r.EntityType = "release_group"
			r.ArtistMBID = artistMBID
			results = append(results, r)
		}
	}

	return results
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
		entries = append(entries, SearchIndexResult{
			EntityType:     "release_group",
			MBID:           rg.MBID,
			Title:          rg.Title,
			ArtistName:     artistName,
			ArtistMBID:     artistMBID,
			Popularity:     0,
			PrimaryType:    rg.PrimaryType,
			SecondaryTypes: strings.Join(rg.SecondaryTypes, ","),
			ReleaseDate:    rg.FirstReleaseDate,
		})
	}

	si.upsertBatch(entries)

	si.logger.Debug("search index: organic add",
		"artist", artistName,
		"releaseGroups", len(rgs),
	)
}

// Search queries the local FTS5 index and returns matches sorted
// by popularity descending.
// ExactMatches returns index rows whose normalized title (or artist
// name) exactly equals the given query.  Used by the top-results
// intent pipeline as a dedicated retrieval source — exact matches
// against high-popularity entities are almost always the right
// answer and should bypass the noise of MB text search.
//
// Returns up to `perCategory` matches per entity type, ordered by
// popularity descending.  Case-insensitive; trims whitespace.
func (si *SearchIndex) ExactMatches(query string, perCategory int) []SearchIndexResult {
	if !si.IsReady() {
		return nil
	}

	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}

	if perCategory <= 0 {
		perCategory = 3
	}

	rows, err := si.db.QueryContext(`
		SELECT entity_type, mbid, title, artist_name, artist_mbid,
		       popularity, listener_count, duration, primary_type,
		       secondary_types, release_date, caa_release_mbid,
		       release_name, artist_type, country, disambiguation,
		       sort_name, in_library, is_similar,
		       COALESCE(local_artist_id, 0),
		       COALESCE(local_release_group_id, 0),
		       COALESCE(local_recording_id, 0)
		FROM explore_index
		WHERE (LOWER(title) = ? OR LOWER(artist_name) = ?)
		  AND popularity > 0
		ORDER BY entity_type, popularity DESC
	`, q, q)
	if err != nil {
		return nil
	}

	defer func() { _ = rows.Close() }()

	// Group by entity type and cap at perCategory each, ordered by
	// popularity desc because the SQL `ORDER BY entity_type, popularity DESC`
	// gives us entity-type buckets already sorted within each.
	buckets := map[string][]SearchIndexResult{
		"artist":        nil,
		"release_group": nil,
		"recording":     nil,
	}

	for rows.Next() {
		var r SearchIndexResult

		if err := rows.Scan(
			&r.EntityType, &r.MBID, &r.Title, &r.ArtistName, &r.ArtistMBID,
			&r.Popularity, &r.ListenerCount, &r.Duration, &r.PrimaryType,
			&r.SecondaryTypes, &r.ReleaseDate, &r.CAAReleaseMBID,
			&r.ReleaseName, &r.ArtistType, &r.Country, &r.Disambiguation,
			&r.SortName, &r.InLibrary, &r.IsSimilar,
			&r.LocalArtistID, &r.LocalReleaseGroupID, &r.LocalRecordingID,
		); err != nil {
			continue
		}

		// For artists, only match on title (name).  For recordings
		// and release groups, match on either title or artist name
		// — that way "miley cyrus" surfaces both the artist and
		// her recordings.
		qLower := strings.ToLower(q)
		titleMatch := strings.ToLower(r.Title) == qLower
		artistMatch := strings.ToLower(r.ArtistName) == qLower

		if r.EntityType == "artist" && !titleMatch {
			continue
		}

		if r.EntityType != "artist" && !titleMatch && !artistMatch {
			continue
		}

		bucket := buckets[r.EntityType]
		if len(bucket) >= perCategory {
			continue
		}

		buckets[r.EntityType] = append(bucket, r)
	}

	var out []SearchIndexResult
	out = append(out, buckets["artist"]...)
	out = append(out, buckets["release_group"]...)
	out = append(out, buckets["recording"]...)

	return out
}

func (si *SearchIndex) Search(query string, limit int) []SearchIndexResult {	if !si.IsReady() {
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
		       i.artist_mbid, i.popularity, i.listener_count,
		       i.duration, i.primary_type, i.secondary_types, i.release_date,
		       i.caa_release_mbid, i.release_name,
		       i.artist_type, i.country, i.disambiguation, i.sort_name,
		       i.in_library, i.is_similar,
		       COALESCE(i.local_artist_id, 0),
		       COALESCE(i.local_release_group_id, 0),
		       COALESCE(i.local_recording_id, 0)
		FROM explore_index i
		JOIN explore_index_fts f ON f.rowid = i.id
		WHERE explore_index_fts MATCH ?
		ORDER BY bm25(explore_index_fts, 3.0, 1.0, 0.5)
		         - (ln(i.popularity + 1) * 1.5)
		         - (i.in_library * 3.0)
		         - (i.is_similar * 1.5)
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

		if err := rows.Scan(
			&r.EntityType, &r.MBID, &r.Title, &r.ArtistName,
			&r.ArtistMBID, &r.Popularity, &r.ListenerCount,
			&r.Duration, &r.PrimaryType, &r.SecondaryTypes, &r.ReleaseDate,
			&r.CAAReleaseMBID, &r.ReleaseName,
			&r.ArtistType, &r.Country, &r.Disambiguation, &r.SortName,
			&r.InLibrary, &r.IsSimilar,
			&r.LocalArtistID, &r.LocalReleaseGroupID, &r.LocalRecordingID,
		); err != nil {
			si.logger.Warn("search index scan error", "error", err)

			continue
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

	// Initialize tier status for the UI.
	si.mu.Lock()
	si.buildStatus = IndexStatus{
		Building: true,
		Tiers: []TierStatus{
			{Name: "Sitewide Top Lists", State: "pending"},
			{Name: "Sitewide Discographies", State: "pending"},
			{Name: "Library Artists", State: "pending"},
			{Name: "Similar Artists", State: "pending"},
			{Name: "Popularity Backfill", State: "pending"},
		},
	}
	si.mu.Unlock()

	// Mark ready from existing rows so search works during the build.
	si.MarkReadyIfPopulated()

	indexLimiter := NewRateLimiterN(indexerRate)
	indexLB := NewListenBrainzClient(indexLimiter, si.lb.cache, si.logger.WithGroup("indexer"))

	// Tier 1: sitewide instant — refresh weekly (12 calls, <5s).
	tier1Fresh := si.isMetaFresh("tier1_built", indexTier1Interval)

	var sitewideArtists []lbSitewideArtist

	if tier1Fresh {
		si.logger.Info("search index: Tier 1 fresh, loading cached artists")
		si.setTierStatus("Sitewide Top Lists", "skipped", 0, 0)

		sitewideArtists = si.loadCachedSitewideArtists()
	} else {
		si.setTierStatus("Sitewide Top Lists", "running", 12, 0)
		sitewideArtists = si.buildTier1Sitewide(ctx, indexLB)

		if ctx.Err() != nil {
			return
		}

		si.setMeta("tier1_built", time.Now().UTC().Format(time.RFC3339))
		si.setTierStatus("Sitewide Top Lists", "complete", 12, 12)
	}

	si.mu.Lock()
	si.ready = true
	si.mu.Unlock()

	si.refreshStatusCounts()
	si.logger.Info("search index: Tier 1 complete (sitewide instant)")

	// Tiers 2-4: discographies — refresh monthly, incremental.
	// Only fetch discographies for artists not already indexed.
	// Each tier's timestamp is tracked independently so progress
	// survives app restarts mid-build.
	tier2Fresh := si.isMetaFresh("tier2_built", indexTier2Interval)
	tier3Fresh := si.isMetaFresh("tier3_built", indexTier2Interval)
	tier4Fresh := si.isMetaFresh("tier4_built", indexTier2Interval)

	// Repair pass: runs unconditionally (outside the tier-fresh
	// gate) so gaps from previous incomplete runs are healed even
	// when the tier timestamps claim the build is fresh.  Any
	// artist row with discog_fetched=0 — including those created
	// by AddFromCache during a frontend visit, or left over from
	// a crash mid-build — gets its full discography pulled here.
	{
		indexedForRepair := si.indexedArtistMBIDs()
		if unindexed := si.unindexedArtistEntries(indexedForRepair); len(unindexed) > 0 {
			si.logger.Info("search index: repair pass starting",
				"unindexedArtists", len(unindexed),
			)

			si.setTierStatus("Repair Discographies", "running", len(unindexed), 0)
			si.indexArtistDiscographies(ctx, indexLB, unindexed, "Repair", false)

			if ctx.Err() != nil {
				return
			}

			si.setTierStatus("Repair Discographies", "complete", len(unindexed), len(unindexed))
			si.refreshStatusCounts()
			si.logger.Info("search index: repair pass complete",
				"artists", len(unindexed),
			)
		} else {
			si.setTierStatus("Repair Discographies", "skipped", 0, 0)
		}
	}

	if tier2Fresh && tier3Fresh && tier4Fresh {
		si.logger.Info("search index: discographies fresh, skipping Tiers 2-4")
	} else {
		indexed := si.indexedArtistMBIDs()

		var libraryMBIDs []string

		// Tier 2: sitewide artists' discographies (incremental).
		if tier2Fresh {
			si.logger.Info("search index: Tier 2 fresh, skipping")
			si.setTierStatus("Sitewide Discographies", "skipped", 0, 0)
		} else {
			newSitewide := filterUnindexed(sitewideArtists, indexed)

			si.logger.Info("search index: Tier 2 starting",
				"total", len(sitewideArtists),
				"alreadyIndexed", len(sitewideArtists)-len(newSitewide),
				"new", len(newSitewide),
			)

			si.setTierStatus("Sitewide Discographies", "running", len(newSitewide), 0)
			si.indexArtistDiscographies(ctx, indexLB, newSitewide, "Tier 2", false)

			if ctx.Err() != nil {
				return
			}

			si.setMeta("tier2_built", time.Now().UTC().Format(time.RFC3339))
			si.setTierStatus("Sitewide Discographies", "complete", len(newSitewide), len(newSitewide))
			si.refreshStatusCounts()
			si.logger.Info("search index: Tier 2 complete (sitewide discographies)")
		}

		// Tier 3: library artists' discographies (incremental).
		if tier3Fresh {
			si.logger.Info("search index: Tier 3 fresh, skipping")
			si.setTierStatus("Library Artists", "skipped", 0, 0)
		} else {
			si.setTierStatus("Library Artists", "running", 0, 0)
			indexed = si.indexedArtistMBIDs()
			libraryMBIDs = si.buildTier3Library(ctx, indexLB, sitewideArtists, indexed)

			if ctx.Err() != nil {
				return
			}

			si.setMeta("tier3_built", time.Now().UTC().Format(time.RFC3339))
			si.setTierStatus("Library Artists", "complete", len(libraryMBIDs), len(libraryMBIDs))
			si.logger.Info("search index: Tier 3 complete (library discographies)")
		}

		// Tier 4: similar artists (incremental).
		if tier4Fresh {
			si.logger.Info("search index: Tier 4 fresh, skipping")
			si.setTierStatus("Similar Artists", "skipped", 0, 0)
		} else {
			if libraryMBIDs == nil {
				// Tier 3 was skipped, load library MBIDs for Tier 4.
				libraryMBIDs = si.getLibraryArtistMBIDs()
			}

			indexed = si.indexedArtistMBIDs()
			si.setTierStatus("Similar Artists", "running", len(libraryMBIDs), 0)
			si.buildTier4Similar(ctx, indexLB, libraryMBIDs, indexed)

			if ctx.Err() != nil {
				return
			}

			si.setMeta("tier4_built", time.Now().UTC().Format(time.RFC3339))
			si.setTierStatus("Similar Artists", "complete", len(libraryMBIDs), len(libraryMBIDs))
			si.refreshStatusCounts()
			si.logger.Info("search index: Tier 4 complete (similar artists)")
		}
	}

	// Tier 5: backfill popularity for entities with missing data.
	tier5Fresh := si.isMetaFresh("tier5_built", indexTier1Interval)
	if tier5Fresh {
		si.setTierStatus("Popularity Backfill", "skipped", 0, 0)
	} else {
		si.setTierStatus("Popularity Backfill", "running", 0, 0)
		si.buildTier5Popularity(ctx, indexLB)
		if ctx.Err() != nil {
			return
		}

		si.setMeta("tier5_built", time.Now().UTC().Format(time.RFC3339))
		si.setTierStatus("Popularity Backfill", "complete", 0, 0)
		si.logger.Info("search index: Tier 5 complete (popularity backfill)")
	}

	si.mu.Lock()
	si.buildStatus.Building = false
	si.mu.Unlock()

	// Populate local library cross-reference columns so every
	// read path has O(1) access to "do I own this?"
	si.PopulateLocalCrossReferences()

	si.refreshStatusCounts()
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
			// Popularity intentionally 0 — backfilled by Tier 5 with the
			// uncapped total_listen_count from the popularity API.
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
			// Popularity intentionally 0 — backfilled by Tier 5.
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

	// Read local library artists — prefer direct MBIDs from tags,
	// fall back to name matching against the sitewide/index map.
	libRows, err := si.db.QueryContext(
		"SELECT DISTINCT name, mbid FROM artists",
	)
	if err != nil {
		si.logger.Warn("search index: library artists query failed", "error", err)

		return nil
	}

	defer func() { _ = libRows.Close() }()

	var matched []lbSitewideArtist

	var resolvedMBIDs []string

	for libRows.Next() {
		var name string

		var mbidPtr *string

		if err := libRows.Scan(&name, &mbidPtr); err != nil {
			continue
		}

		// Direct MBID from tags — most reliable.
		if mbidPtr != nil && *mbidPtr != "" {
			mbid := *mbidPtr
			resolvedMBIDs = append(resolvedMBIDs, mbid)

			if !indexed[mbid] {
				matched = append(matched, lbSitewideArtist{
					ArtistMBID: mbid,
					ArtistName: name,
				})
			}

			continue
		}

		// Fall back to name matching.
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

			if !indexed[a.ArtistMBID] {
				matched = append(matched, a)
			}
		}
	}

	if len(matched) > 0 {
		si.indexArtistDiscographies(ctx, lb, matched, "Tier 3", true)

		// Mark all Tier 3 entries as in_library.
		si.markInLibrary(matched)
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

	// Fetch similar artists in batches of seeds, fanned out
	// concurrently.  The labs multi-seed POST form is broken and
	// returns mis-grouped results, so fetchSimilarArtistsBatch
	// actually makes one GET per seed (see its comment).  Batches
	// keep the log output bounded.
	newArtistMap := make(map[string]lbSitewideArtist)

	for i := 0; i < len(libraryMBIDs); i += similarArtistsBatchSize {
		if ctx.Err() != nil {
			break
		}

		end := i + similarArtistsBatchSize
		if end > len(libraryMBIDs) {
			end = len(libraryMBIDs)
		}

		batch := libraryMBIDs[i:end]
		grouped := si.fetchSimilarArtistsBatch(ctx, lb, batch)

		// Persist similarity relationships per seed.
		for _, seedMBID := range batch {
			similar := grouped[seedMBID]
			si.storeSimilarArtists(seedMBID, similar)

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
		}

		si.logger.Info("search index: Tier 4 similar batch complete",
			"batch", (i/similarArtistsBatchSize)+1,
			"totalBatches", (len(libraryMBIDs)+similarArtistsBatchSize-1)/similarArtistsBatchSize,
			"processed", end,
		)
	}

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

	// Index artist rows only (no discography) — similar artists
	// are mostly obscure and their per-track data rarely surfaces
	// in searches.  Discographies are fetched on-demand when the
	// user drills into an artist detail view.  This saves ~2 API
	// calls per artist (~10K total for Tier 4).
	si.indexArtistDiscographies(ctx, lb, newArtists, "Tier 4", false)

	// Mark all Tier 4 entries as similar.
	si.markSimilar(newArtists)
}

type lbSimilarArtistWire struct {
	ArtistMBID    string `json:"artist_mbid"`
	Name          string `json:"name"`
	Score         int    `json:"score"`
	ReferenceMBID string `json:"reference_mbid"` // which seed artist this result belongs to
}

// fetchSimilarArtistsBatch queries the labs similar-artists endpoint
// for multiple seed MBIDs.  Despite the name, this actually fans
// out one request per seed: the labs API's multi-seed mode is
// broken (results for different seeds get mis-labeled, and some
// seeds return zero), so batching with multiple artist_mbids is
// not viable.  Concurrency is bounded by indexerRate to respect
// the labs rate limit; each call goes through the provided LB
// client's rate limiter and cache.
func (si *SearchIndex) fetchSimilarArtistsBatch(
	ctx context.Context, lb *ListenBrainzClient, seedMBIDs []string,
) map[string][]lbSimilarArtistWire {
	if len(seedMBIDs) == 0 {
		return nil
	}

	var (
		mu      sync.Mutex
		grouped = make(map[string][]lbSimilarArtistWire, len(seedMBIDs))
		wg      sync.WaitGroup
	)

	sem := make(chan struct{}, indexerRate)

	for _, seedMBID := range seedMBIDs {
		if ctx.Err() != nil {
			break
		}

		sem <- struct{}{}

		wg.Add(1)

		go func(seed string) {
			defer func() {
				<-sem
				wg.Done()
			}()

			// Use the LB client's per-seed GET form — goes through
			// the shared rate limiter and cache.  The multi-seed
			// POST form is not viable (see function comment).
			similar, err := lb.SimilarArtists(ctx, seed)
			if err != nil || len(similar) == 0 {
				return
			}

			// Convert to the internal wire type used by the caller
			// and trim to indexSimilarPerArtist.
			if len(similar) > indexSimilarPerArtist {
				similar = similar[:indexSimilarPerArtist]
			}

			results := make([]lbSimilarArtistWire, len(similar))
			for i, s := range similar {
				results[i] = lbSimilarArtistWire{
					ArtistMBID:    s.ArtistMBID,
					Name:          s.Name,
					Score:         int(s.Score),
					ReferenceMBID: seed,
				}
			}

			mu.Lock()
			grouped[seed] = results
			mu.Unlock()
		}(seedMBID)
	}

	wg.Wait()

	return grouped
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
	forceMax bool,
) {
	if len(artists) == 0 {
		return
	}

	// Prefetch artist metadata in batches of 1000 — one GET per batch
	// instead of one per artist.  Populates type, country, and writes
	// artist rows with these fields up-front.  This runs to completion
	// before the discography loop so indexOneArtist can read from
	// the batch results via the metadata cache.
	si.prefetchArtistMetadata(ctx, lb, artists)

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

			si.indexOneArtist(ctx, lb, artist, forceMax)

			n := completed.Add(1)

			// Update tier status for the UI.
			tierName := tier // "Tier 2" or "Tier 3"
			switch tier {
			case "Tier 2":
				tierName = "Sitewide Discographies"
			case "Tier 3":
				tierName = "Library Artists"
			}

			si.setTierStatus(tierName, "running", len(artists), int(n))

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

// prefetchArtistMetadata batch-fetches artist metadata from LB's
// /1/metadata/artist/ endpoint and writes artist rows up-front.
// This populates type and country for all artists in a single
// GET per 1000-artist chunk, rather than requiring per-artist
// MB calls.  Aliases, disambiguation, and sort_name still come
// from the per-artist MB fetch during image resolution — unless
// we can synthesize a satisfactory cached response.
//
// The prefetch also pre-populates the mb:artist-rels cache with
// a synthesized envelope derived from LB data, so the per-artist
// MB call is skipped entirely for artists where we have LB data.
// Aliases/disambiguation won't be available, but type/country/
// name and wikidata QID (for image resolution) will be.
func (si *SearchIndex) prefetchArtistMetadata(
	ctx context.Context,
	lb *ListenBrainzClient,
	artists []lbSitewideArtist,
) {
	const batchSize = 1000

	var (
		processed atomic.Int32
		batchWG   sync.WaitGroup
	)

	// Build a lookup from mbid to artist name.
	nameByMBID := make(map[string]string, len(artists))
	for _, a := range artists {
		nameByMBID[a.ArtistMBID] = a.ArtistName
	}

	mbids := make([]string, 0, len(artists))
	for _, a := range artists {
		if a.ArtistMBID != "" {
			mbids = append(mbids, a.ArtistMBID)
		}
	}

	for i := 0; i < len(mbids); i += batchSize {
		if ctx.Err() != nil {
			return
		}

		end := i + batchSize
		if end > len(mbids) {
			end = len(mbids)
		}

		batch := mbids[i:end]
		batchWG.Add(1)

		go func(chunk []string) {
			defer batchWG.Done()

			meta, err := lb.BatchArtistMetadata(ctx, chunk)
			if err != nil || len(meta) == 0 {
				return
			}

			// Upsert artist rows with the LB-sourced fields.
			entries := make([]SearchIndexResult, 0, len(meta))

			for mbid, m := range meta {
				name := nameByMBID[mbid]
				if name == "" {
					name = m.Name
				}

				entries = append(entries, SearchIndexResult{
					EntityType: "artist",
					MBID:       mbid,
					Title:      name,
					ArtistName: name,
					ArtistMBID: mbid,
					ArtistType: m.Type,
					Country:    m.Country,
				})

				// Synthesize an MB artist-rels cache entry so
				// fetchMBRels skips the per-artist network call.
				// Contains only the wikidata URL (for image
				// resolution) and the type/country/name that
				// GetArtistDetails reads.  Aliases and
				// disambiguation are empty — those come from
				// an on-demand MB lookup later if needed.
				if si.artistImg != nil {
					si.artistImg.PreloadArtistRels(mbid, m)
				}
			}

			si.upsertBatch(entries)
			processed.Add(int32(len(entries)))
		}(batch)
	}

	batchWG.Wait()

	si.logger.Info("search index: prefetched artist metadata",
		"artists", len(mbids),
		"processed", processed.Load(),
	)
}

func (si *SearchIndex) indexOneArtist(
	ctx context.Context,
	lb *ListenBrainzClient,
	artist lbSitewideArtist,
	forceMax bool,
) {
	if ctx.Err() != nil {
		return
	}

	rgLimit, recLimit := si.scaledLimits(artist.ListenCount, forceMax)

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

	// Write the artist entry into the index so indexedArtistMBIDs()
	// recognises this artist as processed on subsequent builds.
	// Only mark DiscogFetched=true if at least one of the discography
	// fetches actually returned data — a transient API failure should
	// allow a retry on the next build, not permanently claim the
	// artist as indexed.  Also stores aliases and detail fields from
	// the now-cached MB rels (populated by the image resolution above)
	// for FTS search.
	if si.artistImg != nil {
		gotData := len(rgs) > 0 || len(recs) > 0
		artistEntry := SearchIndexResult{
			EntityType:    "artist",
			MBID:          artist.ArtistMBID,
			Title:         artist.ArtistName,
			ArtistName:    artist.ArtistName,
			ArtistMBID:    artist.ArtistMBID,
			Popularity:    artist.ListenCount,
			DiscogFetched: gotData,
		}

		if details := si.artistImg.GetArtistDetails(artist.ArtistMBID); details != nil {
			artistEntry.ArtistType = details.Type
			artistEntry.Country = details.Country
			artistEntry.Disambiguation = details.Disambiguation
			artistEntry.SortName = details.SortName
			artistEntry.Aliases = details.Aliases
		}

		si.upsertBatch([]SearchIndexResult{artistEntry})
	}

	// Batch write discography results.
	all := make([]SearchIndexResult, 0, len(rgs)+len(recs))
	all = append(all, rgs...)
	all = append(all, recs...)

	for i := 0; i < len(all); i += indexBatchSize {
		end := i + indexBatchSize
		if end > len(all) {
			end = len(all)
		}

		si.upsertBatch(all[i:end])
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
		ReleaseGroupMBID string `json:"release_group_mbid"`
		TotalListenCount int    `json:"total_listen_count"`
		ReleaseGroup     struct {
			Name           string `json:"name"`
			Type           string `json:"type"`
			Date           string `json:"date"`
			CAAReleaseMBID string `json:"caa_release_mbid"`
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

		results = append(results, SearchIndexResult{
			EntityType:     "release_group",
			MBID:           r.ReleaseGroupMBID,
			Title:          r.ReleaseGroup.Name,
			ArtistName:     artistName,
			ArtistMBID:     artistMBID,
			Popularity:     r.TotalListenCount,
			PrimaryType:    r.ReleaseGroup.Type,
			ReleaseDate:    r.ReleaseGroup.Date,
			CAAReleaseMBID: r.ReleaseGroup.CAAReleaseMBID,
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
			EntityType:     "recording",
			MBID:           r.RecordingMBID,
			Title:          r.RecordingName,
			ArtistName:     r.ArtistName,
			ArtistMBID:     artist.ArtistMBID,
			Popularity:     r.TotalListenCount,
			Duration:       r.Length,
			CAAReleaseMBID: r.CAAReleaseMBID,
			ReleaseName:    r.ReleaseName,
		})
	}

	return results
}

// ---------------------------------------------------------------------------
// Tier 5: popularity backfill
// ---------------------------------------------------------------------------

// popularityBatchSize is the number of MBIDs per LB popularity request.
// LB accepts up to 1000 per POST call.
const popularityBatchSize = 1000

// buildTier5Popularity batch-queries LB popularity for every entity in the
// index that has popularity = 0, then writes results back via BackfillPopularity.
func (si *SearchIndex) buildTier5Popularity(ctx context.Context, lb *ListenBrainzClient) {
	type entityKind struct {
		entityType string
		fetch      func(context.Context, []string) (map[string]PopularityData, error)
	}

	kinds := []entityKind{
		{"artist", lb.ArtistPopularity},
		{"release_group", lb.ReleaseGroupPopularity},
		{"recording", lb.RecordingPopularity},
	}

	for _, kind := range kinds {
		if ctx.Err() != nil {
			return
		}

		mbids := si.mbidsWithoutPopularity(kind.entityType)
		if len(mbids) == 0 {
			si.logger.Info("search index: Tier 5 skipped (no missing popularity)",
				"entityType", kind.entityType)

			continue
		}

		batches := chunkStrings(mbids, popularityBatchSize)

		si.logger.Info("search index: Tier 5 starting",
			"entityType", kind.entityType,
			"entities", len(mbids),
			"batches", len(batches),
		)

		var filled int

		sem := make(chan struct{}, indexerRate)

		for i, batch := range batches {
			if ctx.Err() != nil {
				return
			}

			sem <- struct{}{}

			pops, err := kind.fetch(ctx, batch)

			<-sem

			if err != nil {
				si.logger.Warn("search index: Tier 5 batch failed",
					"entityType", kind.entityType,
					"batch", i+1,
					"error", err,
				)

				continue
			}

			si.BackfillPopularity(pops)
			filled += len(pops)

			if (i+1)%indexProgressInterval == 0 {
				si.logger.Info("search index: Tier 5 progress",
					"entityType", kind.entityType,
					"batches", fmt.Sprintf("%d/%d", i+1, len(batches)),
					"filled", filled,
				)
			}
		}

		si.logger.Info("search index: Tier 5 entity type done",
			"entityType", kind.entityType,
			"filled", filled,
			"batches", len(batches),
		)
	}
}

// mbidsWithoutPopularity returns all MBIDs in the index for the given
// entity type that have popularity = 0.
func (si *SearchIndex) mbidsWithoutPopularity(entityType string) []string {
	rows, err := si.db.QueryContext(
		"SELECT mbid FROM explore_index WHERE entity_type = ? AND popularity = 0",
		entityType,
	)
	if err != nil {
		si.logger.Warn("search index: failed to query unpopulated MBIDs",
			"entityType", entityType, "error", err)

		return nil
	}

	defer func() { _ = rows.Close() }()

	var mbids []string

	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err == nil {
			mbids = append(mbids, m)
		}
	}

	return mbids
}

// chunkStrings splits a slice into chunks of at most size n.
func chunkStrings(s []string, n int) [][]string {
	var chunks [][]string

	for i := 0; i < len(s); i += n {
		end := i + n
		if end > len(s) {
			end = len(s)
		}

		chunks = append(chunks, s[i:end])
	}

	return chunks
}

// ---------------------------------------------------------------------------
// Database writes
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Unified write API
// ---------------------------------------------------------------------------
//
// All writes to explore_index go through upsertBatch.  There are no
// side-channel write paths — if data needs to land in the index, it
// flows through a SearchIndexResult struct that carries every field.
// The merge semantics are: non-empty incoming values replace existing
// empty values, and numeric fields use "highest wins" for popularity/
// listener_count/duration so older richer data survives refreshes.

// upsertArtists is a convenience wrapper for Tier 1 sitewide artists.
// Writes them as artist rows with popularity=0 — the actual popularity
// (uncapped total_listen_count) is filled in by Tier 5 via the
// POST popularity API.  The sitewide listen_count is a capped
// different metric we don't want to mix in.
func (si *SearchIndex) upsertArtists(artists []lbSitewideArtist) {
	batch := make([]SearchIndexResult, 0, indexBatchSize)

	for _, a := range artists {
		batch = append(batch, SearchIndexResult{
			EntityType: "artist",
			MBID:       a.ArtistMBID,
			Title:      a.ArtistName,
			ArtistName: a.ArtistName,
			ArtistMBID: a.ArtistMBID,
			// Popularity intentionally 0 — backfilled by Tier 5.
		})

		if len(batch) >= indexBatchSize {
			si.upsertBatch(batch)
			batch = batch[:0]
		}
	}

	if len(batch) > 0 {
		si.upsertBatch(batch)
	}
}

// upsertSearchResults chunks large batches into transactions of
// indexBatchSize and flushes each via upsertBatch.
func (si *SearchIndex) upsertSearchResults(results []SearchIndexResult) {
	for i := 0; i < len(results); i += indexBatchSize {
		end := i + indexBatchSize
		if end > len(results) {
			end = len(results)
		}

		si.upsertBatch(results[i:end])
	}
}

// upsertBatch writes a batch of SearchIndexResult entries to the index
// inside a single transaction.  This is the ONE function that all
// writes go through.  All fields are handled — callers don't need to
// know which columns exist for which entity types.
func (si *SearchIndex) upsertBatch(entries []SearchIndexResult) {
	if len(entries) == 0 {
		return
	}

	tx, err := si.db.BeginTx()
	if err != nil {
		si.logger.Warn("search index: begin tx error", "error", err)

		return
	}

	for _, e := range entries {
		if e.MBID == "" {
			continue // skip entries without MBIDs — can't be looked up
		}

		inLib := 0
		if e.InLibrary {
			inLib = 1
		}

		isSim := 0
		if e.IsSimilar {
			isSim = 1
		}

		discogFetched := 0
		if e.DiscogFetched {
			discogFetched = 1
		}

		if _, err := tx.Exec(`
			INSERT INTO explore_index (
				entity_type, mbid, title, artist_name, artist_mbid, aliases,
				popularity, listener_count,
				duration, caa_release_mbid, release_name,
				primary_type, secondary_types, release_date,
				artist_type, country, disambiguation, sort_name,
				in_library, is_similar,
				local_artist_id, local_release_group_id, local_recording_id,
				discog_fetched,
				schema_version
			) VALUES (
				?, ?, ?, ?, ?, ?,
				?, ?,
				?, ?, ?,
				?, ?, ?,
				?, ?, ?, ?,
				?, ?,
				NULLIF(?, 0), NULLIF(?, 0), NULLIF(?, 0),
				?,
				?
			)
			ON CONFLICT(mbid) DO UPDATE SET
				-- Title and artist info: don't clobber a good value with
				-- an empty string or with the MBID itself (which can sneak
				-- in via fallback paths in AddFromCache).
				title = CASE
					WHEN excluded.title != '' AND excluded.title != excluded.mbid THEN excluded.title
					ELSE title
				END,
				artist_name = CASE
					WHEN excluded.artist_name != '' AND excluded.artist_name != excluded.artist_mbid THEN excluded.artist_name
					ELSE artist_name
				END,
				artist_mbid = CASE WHEN excluded.artist_mbid != '' THEN excluded.artist_mbid ELSE artist_mbid END,
				aliases     = CASE WHEN excluded.aliases != '' THEN excluded.aliases ELSE aliases END,

				-- Highest wins for popularity + listener_count (refreshes can go up).
				popularity     = CASE WHEN excluded.popularity > popularity THEN excluded.popularity ELSE popularity END,
				listener_count = CASE WHEN excluded.listener_count > listener_count THEN excluded.listener_count ELSE listener_count END,

				-- Non-empty wins for all other optional fields (never clobber with empty).
				duration         = CASE WHEN excluded.duration > 0 THEN excluded.duration ELSE duration END,
				caa_release_mbid = CASE WHEN excluded.caa_release_mbid != '' THEN excluded.caa_release_mbid ELSE caa_release_mbid END,
				release_name     = CASE WHEN excluded.release_name != '' THEN excluded.release_name ELSE release_name END,
				primary_type     = CASE WHEN excluded.primary_type != '' THEN excluded.primary_type ELSE primary_type END,
				secondary_types  = CASE WHEN excluded.secondary_types != '' THEN excluded.secondary_types ELSE secondary_types END,
				release_date     = CASE WHEN excluded.release_date != '' THEN excluded.release_date ELSE release_date END,
				artist_type      = CASE WHEN excluded.artist_type != '' THEN excluded.artist_type ELSE artist_type END,
				country          = CASE WHEN excluded.country != '' THEN excluded.country ELSE country END,
				disambiguation   = CASE WHEN excluded.disambiguation != '' THEN excluded.disambiguation ELSE disambiguation END,
				sort_name        = CASE WHEN excluded.sort_name != '' THEN excluded.sort_name ELSE sort_name END,

				-- Flags and cross-references: non-null wins.
				in_library             = MAX(in_library, excluded.in_library),
				is_similar             = MAX(is_similar, excluded.is_similar),
				discog_fetched         = MAX(discog_fetched, excluded.discog_fetched),
				local_artist_id        = COALESCE(excluded.local_artist_id, local_artist_id),
				local_release_group_id = COALESCE(excluded.local_release_group_id, local_release_group_id),
				local_recording_id     = COALESCE(excluded.local_recording_id, local_recording_id),

				schema_version = MAX(schema_version, excluded.schema_version)
		`,
			e.EntityType, e.MBID, e.Title, e.ArtistName, e.ArtistMBID, e.Aliases,
			e.Popularity, e.ListenerCount,
			e.Duration, e.CAAReleaseMBID, e.ReleaseName,
			e.PrimaryType, e.SecondaryTypes, e.ReleaseDate,
			e.ArtistType, e.Country, e.Disambiguation, e.SortName,
			inLib, isSim,
			e.LocalArtistID, e.LocalReleaseGroupID, e.LocalRecordingID,
			discogFetched,
			currentSchemaVersion,
		); err != nil {
			si.logger.Warn("search index: upsert error",
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

// unindexedArtistEntries returns artist rows that exist in the
// index but haven't had their full discography fetched yet
// (discog_fetched = 0).  Excludes anything in the indexed set.
// Used by the repair pass to heal gaps from AddFromCache or
// from previous incomplete runs.
func (si *SearchIndex) unindexedArtistEntries(indexed map[string]bool) []lbSitewideArtist {
	rows, err := si.db.QueryContext(`
		SELECT mbid, title, popularity
		FROM explore_index
		WHERE entity_type = 'artist' AND discog_fetched = 0
	`)
	if err != nil {
		return nil
	}

	defer func() { _ = rows.Close() }()

	var out []lbSitewideArtist

	for rows.Next() {
		var (
			mbid string
			name string
			pop  int
		)

		if err := rows.Scan(&mbid, &name, &pop); err != nil {
			continue
		}

		if indexed[mbid] {
			continue
		}

		out = append(out, lbSitewideArtist{
			ArtistMBID:  mbid,
			ArtistName:  name,
			ListenCount: pop,
		})
	}

	return out
}

// indexedArtistMBIDs returns artist MBIDs that have had their full
// discography fetched by the indexer pipeline.  Used by tier 2/3 to
// skip artists already processed.  Excludes artist rows that only
// got into the index via AddFromCache (frontend organic growth) —
// those are missing recordings and need a real indexer pass.
func (si *SearchIndex) indexedArtistMBIDs() map[string]bool {
	rows, err := si.db.QueryContext(
		"SELECT DISTINCT artist_mbid FROM explore_index WHERE entity_type = 'artist' AND discog_fetched = 1",
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

// getLibraryArtistMBIDs returns MBIDs for all library artists that have one.
// Used when Tier 3 was skipped but Tier 4 needs the library MBID list.
func (si *SearchIndex) getLibraryArtistMBIDs() []string {
	rows, err := si.db.QueryContext(
		"SELECT DISTINCT mbid FROM artists WHERE mbid IS NOT NULL AND mbid != ''",
	)
	if err != nil {
		return nil
	}

	defer func() { _ = rows.Close() }()

	var mbids []string

	for rows.Next() {
		var mbid string
		if err := rows.Scan(&mbid); err == nil {
			mbids = append(mbids, mbid)
		}
	}

	return mbids
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

// markInLibrary sets in_library=1 for all index entries whose
// artist_mbid matches one of the given artists.  Also populates
// the local_artist_id cross-reference column.
func (si *SearchIndex) markInLibrary(artists []lbSitewideArtist) {
	for _, a := range artists {
		_, _ = si.db.ExecContext(
			`UPDATE explore_index
			 SET in_library = 1,
			     local_artist_id = (SELECT id FROM artists WHERE mbid = ?)
			 WHERE artist_mbid = ?`,
			a.ArtistMBID, a.ArtistMBID,
		)
	}
}

// PopulateLocalCrossReferences walks the library tables and updates
// explore_index rows to set local_*_id columns for any MBIDs that
// exist locally.  Call after a library scan completes.
func (si *SearchIndex) PopulateLocalCrossReferences() {
	// Artists.
	if _, err := si.db.ExecContext(`
		UPDATE explore_index
		SET local_artist_id = (
			SELECT a.id FROM artists a
			WHERE a.mbid = explore_index.mbid
		),
		in_library = CASE
			WHEN EXISTS (SELECT 1 FROM artists WHERE mbid = explore_index.mbid)
			THEN 1 ELSE in_library
		END
		WHERE entity_type = 'artist'
		  AND mbid IN (SELECT mbid FROM artists WHERE mbid IS NOT NULL AND mbid != '')
	`); err != nil {
		si.logger.Warn("cross-ref: update artists failed", "error", err)
	}

	// Release groups.
	if _, err := si.db.ExecContext(`
		UPDATE explore_index
		SET local_release_group_id = (
			SELECT rg.id FROM release_groups rg
			WHERE rg.mbid = explore_index.mbid
		),
		in_library = CASE
			WHEN EXISTS (SELECT 1 FROM release_groups WHERE mbid = explore_index.mbid)
			THEN 1 ELSE in_library
		END
		WHERE entity_type = 'release_group'
		  AND mbid IN (SELECT mbid FROM release_groups WHERE mbid IS NOT NULL AND mbid != '')
	`); err != nil {
		si.logger.Warn("cross-ref: update release groups failed", "error", err)
	}

	// Recordings.
	if _, err := si.db.ExecContext(`
		UPDATE explore_index
		SET local_recording_id = (
			SELECT r.id FROM recordings r
			WHERE r.mbid = explore_index.mbid
		),
		in_library = CASE
			WHEN EXISTS (SELECT 1 FROM recordings WHERE mbid = explore_index.mbid)
			THEN 1 ELSE in_library
		END
		WHERE entity_type = 'recording'
		  AND mbid IN (SELECT mbid FROM recordings WHERE mbid IS NOT NULL AND mbid != '')
	`); err != nil {
		si.logger.Warn("cross-ref: update recordings failed", "error", err)
	}

	si.logger.Info("cross-ref: populated local_*_id columns")
}

// storeSimilarArtists persists the similar artist relationships
// for a source artist into the similar_artist_map table.
func (si *SearchIndex) storeSimilarArtists(sourceMBID string, similar []lbSimilarArtistWire) {
	if len(similar) == 0 {
		return
	}

	tx, err := si.db.BeginTx()
	if err != nil {
		return
	}

	defer func() { _ = tx.Rollback() }()

	// Clear existing entries for this source to avoid stale data.
	_, _ = tx.Exec(
		"DELETE FROM similar_artist_map WHERE source_artist_mbid = ?",
		sourceMBID,
	)

	for _, s := range similar {
		_, _ = tx.Exec(`
			INSERT OR IGNORE INTO similar_artist_map
				(source_artist_mbid, similar_artist_mbid, similar_artist_name, score)
			VALUES (?, ?, ?, ?)
		`, sourceMBID, s.ArtistMBID, s.Name, s.Score)
	}

	_ = tx.Commit()
}

// markSimilar sets is_similar=1 for all index entries whose
// artist_mbid matches one of the given artists.
func (si *SearchIndex) markSimilar(artists []lbSitewideArtist) {
	for _, a := range artists {
		_, _ = si.db.ExecContext(
			"UPDATE explore_index SET is_similar = 1 WHERE artist_mbid = ?",
			a.ArtistMBID,
		)
	}
}

// PopularityData holds both listen count and listener count for a
// single entity.  Used by BackfillPopularity and the popularity
// pipeline to pass both metrics together.
type PopularityData struct {
	ListenCount   int
	ListenerCount int
}

// BackfillPopularity writes LB popularity values back to the index
// for MBIDs that already exist.  Called after LB API responses so
// subsequent searches use the index instead of re-fetching from LB.
func (si *SearchIndex) BackfillPopularity(updates map[string]PopularityData) {
	if len(updates) == 0 {
		return
	}

	tx, err := si.db.BeginTx()
	if err != nil {
		return
	}

	defer func() { _ = tx.Rollback() }()

	for mbid, data := range updates {
		if data.ListenCount <= 0 {
			continue
		}

		_, _ = tx.Exec(
			`UPDATE explore_index
			 SET popularity = CASE WHEN ? > popularity THEN ? ELSE popularity END,
			     listener_count = CASE WHEN ? > listener_count THEN ? ELSE listener_count END
			 WHERE mbid = ?`,
			data.ListenCount, data.ListenCount,
			data.ListenerCount, data.ListenerCount,
			mbid,
		)
	}

	_ = tx.Commit()
}

// BackfillPopularitySimple is a convenience wrapper for callers that
// only have listen counts (no listener count).
func (si *SearchIndex) BackfillPopularitySimple(updates map[string]int) {
	if len(updates) == 0 {
		return
	}

	full := make(map[string]PopularityData, len(updates))
	for mbid, pop := range updates {
		full[mbid] = PopularityData{ListenCount: pop}
	}

	si.BackfillPopularity(full)
}

// GetSimilarityScores returns the highest similarity score for each
// MBID that appears in similar_artist_map as a similar artist.
// Returns a map of mbid → max similarity score.
func (si *SearchIndex) GetSimilarityScores(mbids []string) map[string]int {
	if len(mbids) == 0 {
		return nil
	}

	placeholders := make([]string, len(mbids))
	args := make([]any, len(mbids))

	for i, m := range mbids {
		placeholders[i] = "?"
		args[i] = m
	}

	query := "SELECT similar_artist_mbid, MAX(score) FROM similar_artist_map WHERE similar_artist_mbid IN (" +
		strings.Join(placeholders, ",") + ") GROUP BY similar_artist_mbid"

	rows, err := si.db.QueryContext(query, args...)
	if err != nil {
		return nil
	}

	defer func() { _ = rows.Close() }()

	result := make(map[string]int, len(mbids))

	for rows.Next() {
		var mbid string
		var score int

		if err := rows.Scan(&mbid, &score); err == nil {
			result[mbid] = score
		}
	}

	return result
}

// InvalidateDiscographies clears the discography build timestamps
// so the next build re-runs Tiers 2-4.
func (si *SearchIndex) InvalidateDiscographies() {
	_, _ = si.db.ExecContext(
		"DELETE FROM explore_index_meta WHERE key IN ('discog_built', 'tier2_built', 'tier3_built', 'tier4_built')",
	)
}

func (si *SearchIndex) setMeta(key, value string) {
	if _, err := si.db.ExecContext(
		"INSERT OR REPLACE INTO explore_index_meta (key, value) VALUES (?, ?)",
		key, value,
	); err != nil {
		si.logger.Warn("search index: set meta error", "key", key, "error", err)
	}
}

// MarkReadyIfPopulated sets the index as ready for querying if it
// already contains data from a previous build.  Called eagerly at
// service creation so the index is queryable before StartBuild runs.
func (si *SearchIndex) MarkReadyIfPopulated() {
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
// If forceMax is true, returns the maximum limits regardless of
// popularity (used for library artists).
func (si *SearchIndex) scaledLimits(listenCount int, forceMax bool) (rgs, recs int) {
	if forceMax {
		return indexMaxRGs, indexMaxRecs
	}

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
