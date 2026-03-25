package explore

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"yellowjacket/backend/database"
)

// Index build parameters.
const (
	// indexRebuildInterval is the minimum time between full rebuilds.
	indexRebuildInterval = 7 * 24 * time.Hour

	// indexTopArtists is the number of artists to fetch from the
	// LB sitewide endpoint.
	indexTopArtists = 1000

	// indexRGsPerArtist is the number of top release groups to
	// store per artist.
	indexRGsPerArtist = 10

	// indexRecsPerArtist is the number of top recordings to store
	// per artist.
	indexRecsPerArtist = 10

	// indexBatchSize is the number of rows per INSERT transaction.
	indexBatchSize = 100

	// indexerRate is the requests-per-second for the background
	// indexer's dedicated rate limiter (LB allows 30/10s).
	indexerRate = 3

	// indexProgressInterval is how often to log progress.
	indexProgressInterval = 100
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

// SearchIndex maintains a local SQLite FTS5 index of popular
// albums and tracks from ListenBrainz.  The index is built in the
// background on startup and enables instant popularity-aware
// search without API calls.
type SearchIndex struct {
	db     *database.DB
	lb     *ListenBrainzClient
	logger *slog.Logger

	cancel context.CancelFunc
	done   chan struct{}

	mu    sync.RWMutex
	ready bool
}

// NewSearchIndex creates a search index backed by the given
// database.  Call StartBuild to kick off the background populate.
func NewSearchIndex(
	db *database.DB,
	lb *ListenBrainzClient,
	logger *slog.Logger,
) *SearchIndex {
	return &SearchIndex{
		db:     db,
		lb:     lb,
		logger: logger,
		done:   make(chan struct{}),
	}
}

// StartBuild launches the background index build goroutine.
// Returns immediately.  Safe to call multiple times (no-op if
// already running).
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

// IsReady returns true once the index has been built at least once
// (either fresh or from a previous run).
func (si *SearchIndex) IsReady() bool {
	si.mu.RLock()
	defer si.mu.RUnlock()

	return si.ready
}

// Search queries the local FTS5 index and returns matches sorted
// by popularity descending.  Returns nil if the index hasn't been
// built yet.
func (si *SearchIndex) Search(query string, limit int) []SearchIndexResult {
	if !si.IsReady() {
		return nil
	}

	if limit <= 0 {
		limit = 20
	}

	// FTS5 match syntax: wrap each word with * for prefix matching.
	// "for you" → "for* you*"
	ftsQuery := buildFTSQuery(query)

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

// buildFTSQuery converts a user query into FTS5 match syntax.
// Each word gets a prefix wildcard: "for you" → "for* you*".
// Quotes and special FTS operators are stripped.
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

// splitWords extracts alphanumeric words from a query, stripping
// FTS5 special characters.
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
		r >= 0x80 // Unicode letters (CJK, etc.)
}

// ---------------------------------------------------------------------------
// Background build
// ---------------------------------------------------------------------------

func (si *SearchIndex) build(ctx context.Context) {
	start := time.Now()

	// Check if a recent build exists.
	if si.isFresh() {
		si.logger.Info("search index is fresh, skipping rebuild")

		si.mu.Lock()
		si.ready = true
		si.mu.Unlock()

		return
	}

	si.logger.Info("search index build starting")

	// Fetch top artists from LB sitewide.
	artists, err := si.fetchTopArtists(ctx)
	if err != nil {
		si.logger.Warn("search index: failed to fetch top artists", "error", err)

		// Mark ready if there are existing rows.
		si.markReadyIfPopulated()

		return
	}

	si.logger.Info("search index: fetched top artists", "count", len(artists))

	// Insert artist rows.
	si.upsertArtists(artists)

	// Build a dedicated LB client with faster rate limit.
	indexLimiter := NewRateLimiterN(indexerRate)
	indexLB := NewListenBrainzClient(indexLimiter, si.lb.cache, si.logger.WithGroup("indexer"))

	// Process artists with bounded concurrency.
	sem := make(chan struct{}, indexerRate)

	var wg sync.WaitGroup

	completed := 0

	for _, a := range artists {
		if ctx.Err() != nil {
			si.logger.Info("search index build cancelled",
				"completed", completed,
				"total", len(artists),
			)

			break
		}

		sem <- struct{}{}

		wg.Add(1)

		go func(artist lbSitewideArtist) {
			defer func() {
				<-sem
				wg.Done()
			}()

			si.indexArtist(ctx, indexLB, artist)

			si.mu.Lock()
			completed++

			if completed%indexProgressInterval == 0 {
				si.logger.Info("search index progress",
					"completed", completed,
					"total", len(artists),
					"pct", fmt.Sprintf("%.0f%%", float64(completed)/float64(len(artists))*100),
				)
			}

			si.mu.Unlock()
		}(a)
	}

	wg.Wait()

	// Update build timestamp.
	si.setMeta("last_built", time.Now().UTC().Format(time.RFC3339))

	si.mu.Lock()
	si.ready = true
	si.mu.Unlock()

	si.logger.Info("search index build complete",
		"artists", len(artists),
		"elapsed", time.Since(start).Round(time.Second),
	)
}

// ---------------------------------------------------------------------------
// LB sitewide artists
// ---------------------------------------------------------------------------

type lbSitewideArtist struct {
	ArtistMBID  string `json:"artist_mbid"`
	ArtistName  string `json:"artist_name"`
	ListenCount int    `json:"listen_count"`
}

func (si *SearchIndex) fetchTopArtists(ctx context.Context) ([]lbSitewideArtist, error) {
	url := fmt.Sprintf(
		"%s/1/stats/sitewide/artists?count=%d&range=all_time",
		listenBrainzBaseURL, indexTopArtists,
	)

	// Use the indexer's own HTTP client (no rate limit for this single call).
	req, err := newLBRequest(ctx, url)
	if err != nil {
		return nil, err
	}

	resp, err := si.lb.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sitewide artists: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	var envelope struct {
		Payload struct {
			Artists []lbSitewideArtist `json:"artists"`
		} `json:"payload"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("sitewide artists decode: %w", err)
	}

	return envelope.Payload.Artists, nil
}

func newLBRequest(ctx context.Context, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", lbUserAgent)

	return req, nil
}

// ---------------------------------------------------------------------------
// Per-artist indexing
// ---------------------------------------------------------------------------

func (si *SearchIndex) indexArtist(
	ctx context.Context,
	lb *ListenBrainzClient,
	artist lbSitewideArtist,
) {
	if ctx.Err() != nil {
		return
	}

	// Fetch top release groups.
	rgs := si.fetchTopReleaseGroups(ctx, lb, artist)

	// Fetch top recordings.
	recs := si.fetchTopRecordings(ctx, lb, artist)

	// Batch upsert.
	si.upsertEntries(rgs, recs)
}

func (si *SearchIndex) fetchTopReleaseGroups(
	ctx context.Context,
	lb *ListenBrainzClient,
	artist lbSitewideArtist,
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
		si.logger.Warn("search index: top RGs unmarshal error",
			"artist", artist.ArtistName,
			"error", err,
		)

		return nil
	}

	limit := indexRGsPerArtist
	if limit > len(raw) {
		limit = len(raw)
	}

	results := make([]SearchIndexResult, 0, limit)

	for _, r := range raw[:limit] {
		artistName := artist.ArtistName
		artistMBID := artist.ArtistMBID

		if len(r.Artist.Artists) > 0 {
			artistName = r.Artist.Artists[0].Name
			artistMBID = r.Artist.Artists[0].ArtistMBID
		}

		extra, _ := json.Marshal(map[string]string{
			"type": r.ReleaseGroup.Type,
		})

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
) []SearchIndexResult {
	url := fmt.Sprintf(
		"%s/1/popularity/top-recordings-for-artist/%s",
		listenBrainzBaseURL, artist.ArtistMBID,
	)

	body, err := lb.doGet(ctx, url)
	if err != nil {
		si.logger.Debug("search index: top recordings failed",
			"artist", artist.ArtistName,
			"error", err,
		)

		return nil
	}

	var raw []lbTopRecordingWire
	if err := json.Unmarshal(body, &raw); err != nil {
		si.logger.Warn("search index: top recordings unmarshal error",
			"artist", artist.ArtistName,
			"error", err,
		)

		return nil
	}

	limit := indexRecsPerArtist
	if limit > len(raw) {
		limit = len(raw)
	}

	results := make([]SearchIndexResult, 0, limit)

	for _, r := range raw[:limit] {
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

func (si *SearchIndex) upsertEntries(rgs, recs []SearchIndexResult) {
	all := make([]SearchIndexResult, 0, len(rgs)+len(recs))
	all = append(all, rgs...)
	all = append(all, recs...)

	// Write in batches.
	for i := 0; i < len(all); i += indexBatchSize {
		end := i + indexBatchSize
		if end > len(all) {
			end = len(all)
		}

		si.writeBatch(all[i:end])
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
// Metadata helpers
// ---------------------------------------------------------------------------

func (si *SearchIndex) isFresh() bool {
	rows, err := si.db.QueryContext(
		"SELECT value FROM explore_index_meta WHERE key = 'last_built'",
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

	return time.Since(t) < indexRebuildInterval
}

func (si *SearchIndex) setMeta(key, value string) {
	if _, err := si.db.ExecContext(
		"INSERT OR REPLACE INTO explore_index_meta (key, value) VALUES (?, ?)",
		key, value,
	); err != nil {
		si.logger.Warn("search index: set meta error",
			"key", key,
			"error", err,
		)
	}
}

func (si *SearchIndex) markReadyIfPopulated() {
	rows, err := si.db.QueryContext(
		"SELECT COUNT(*) FROM explore_index",
	)
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

			si.logger.Info("search index: using existing index",
				"entries", count,
			)
		}
	}
}
