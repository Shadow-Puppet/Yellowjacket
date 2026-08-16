package explore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"

	"yellowjacket/backend/database"
	"yellowjacket/backend/events"
	"yellowjacket/backend/jobs"
)

// Index build parameters.
const (
	// indexMaxRGs is the number of release groups fetched per
	// artist by the API discography path (new library artists).
	indexMaxRGs = 50

	// indexMaxRecs is the number of recordings fetched per artist
	// by the API discography path (new library artists).
	indexMaxRecs = 200

	// indexMinPopularity is the minimum listen count for an entry
	// to be indexed.  Cuts noise from long-tail entries.
	indexMinPopularity = 50

	// championPopThreshold is the popularity floor for a row to join
	// the champion FTS.  Rows below it (unless owned) can never win a
	// generic short-prefix query, whose ranking is popularity-dominated,
	// so excluding them is lossless for those queries.  ~90k rows.
	championPopThreshold = 10000

	// genericMaxTokenLen classifies a query as "generic" when its
	// longest token is at most this many runes.  Such queries ("the",
	// "a", "u2") match a huge slice of the index with no selective term,
	// so they route to the champion tier; anything with a longer token
	// is selective enough that the full index is already fast.
	genericMaxTokenLen = 3

	// Typo-tolerant rescue (see fuzzyRescue).  When ordinary prefix-FTS
	// retrieval returns fewer than fuzzyRescueThinHits rows — usually a
	// misspelled query that shares no token prefix with any indexed name
	// — a bigram-similarity pass over the champion set kicks in.
	// fuzzyRescueMinLen guards against very short queries (too few
	// bigrams to discriminate); fuzzyRescueMinScore is the minimum bigram
	// Dice coefficient for a candidate to count as a plausible match.
	fuzzyRescueThinHits = 3
	fuzzyRescueMinLen   = 4
	fuzzyRescueMinScore = 0.34

	// prefixCacheTTL bounds how long a memoised generic-query result is
	// served before it is recomputed; prefixCacheMaxEntries caps the map.
	prefixCacheTTL        = 10 * time.Minute
	prefixCacheMaxEntries = 512

	// championBuiltKey marks in explore_index_meta that the champion FTS
	// has been populated at least once, so it is trusted across restarts.
	championBuiltKey = "champion_built"

	// localXrefReadyKey marks that PopulateLocalCrossReferences has synced
	// the current library into the index at least once.  The unchanged-
	// library launch path skips the (write-heavy) re-sync when it is set;
	// a scan re-runs the sync unconditionally, and an explore_index wipe
	// clears explore_index_meta with it.
	localXrefReadyKey = "local_xref_ready"

	// lyricsIndexReadyKey marks that the lyrics FTS has been rebuilt from
	// the library at least once.  It is kept in sync incrementally by the
	// LRCLIB backfill thereafter, so the unchanged-library launch path
	// skips the redundant full rebuild when it is set.
	lyricsIndexReadyKey = "lyrics_index_ready"

	// indexBatchSize is the number of rows per INSERT transaction.
	indexBatchSize = 100

	// indexerRate is the requests-per-second for the background
	// indexer's dedicated rate limiter (LB allows 30/10s).
	indexerRate = 3

	// discogBackfillMaxPerRun bounds how many owned artists a single
	// post-scan discography backfill run enriches before yielding.  The
	// remainder stay unenriched (discog_fetched = 0) and are picked up by
	// the next run, so a large first-scan library spreads its enrichment
	// across launches instead of running for the better part of an hour.
	discogBackfillMaxPerRun = 2000

	// discogBackfillWorkers is how many owned artists the backfill has
	// in flight at once.
	//
	// The pass was strictly serial, so an artist's ListenBrainz calls,
	// its MusicBrainz browse pages and every upstream's latency were
	// paid end to end before the next artist began — while the limiters
	// that actually keep us polite are per-host and were idle for most
	// of it.  Concurrency here does not raise the request rate against
	// any origin; it stops one artist's slowest upstream deciding how
	// fast the whole run goes.
	discogBackfillWorkers = 6

	// discogBackfillArtistTimeout bounds one artist's share of a run.
	//
	// The MusicBrainz client retries a 503 up to five times, honouring
	// the server's Retry-After (which it caps at a minute), so a single
	// throttled artist can otherwise hold a worker for longer than a
	// hundred healthy ones take.  A timed-out artist goes unmarked and
	// is retried next run, which is what every other failure here does.
	discogBackfillArtistTimeout = 90 * time.Second

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
	Aliases    string `json:"aliases,omitempty"`

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

	// TotalTracks is the canonical release's track count, or 0 for
	// "the catalog does not say".  It is the denominator the album
	// page cannot get from the files when the library holds no tags
	// for the album, and it is deliberately only a denominator: the
	// tracklist itself is not shipped, because the per-artist track
	// budget truncates it and a truncated tracklist is a confident
	// lie about which tracks exist.
	TotalTracks int `json:"totalTracks"`

	// Artist-specific fields (from MB lookup).
	ArtistType     string `json:"artistType"`
	Country        string `json:"country"`
	Disambiguation string `json:"disambiguation"`
	SortName       string `json:"sortName"`

	// Personalization.
	InLibrary bool `json:"inLibrary"`
	IsSimilar bool `json:"isSimilar"`

	// DiscogFetched marks a row as having had its full MusicBrainz
	// metadata fetched.  For artist rows it means the discography
	// (release groups + recordings) was pulled by the indexer
	// pipeline, and indexedArtistMBIDs() uses it to skip already-
	// processed artists in tier 2/3.  For release_group rows it means
	// LookupReleaseGroup already did its one-time MB enrichment (e.g.
	// secondary_types), so the album page's background lookup fires at
	// most once per RG instead of on every visit.
	DiscogFetched bool `json:"-"`

	// Local library cross-reference (0 if not owned).
	LocalArtistID       int64 `json:"localArtistId,omitempty"`
	LocalReleaseGroupID int64 `json:"localReleaseGroupId,omitempty"`
	LocalRecordingID    int64 `json:"localRecordingId,omitempty"`
}

// lbSitewideArtist is the response shape from the LB sitewide
// top-artists endpoint.
type lbSitewideArtist struct {
	ArtistMBID  string `json:"artist_mbid"`
	ArtistName  string `json:"artist_name"`
	ListenCount int    `json:"listen_count"`
}

// SearchIndex maintains a local SQLite FTS5 index of popular
// albums and tracks.  The index is populated in the background from
// the MetaBrainz dumps (see dumpimport.go) and patched incrementally
// via the ListenBrainz API:
//
//   - Initial build: listens dump (popularity) + canonical dump (catalog)
//   - Post-scan: API discographies for new library artists
//   - Ongoing: organic growth from user browsing (AddFromCache)
type SearchIndex struct {
	db *database.DB
	lb *ListenBrainzClient
	// mb is wired after construction (SetMusicBrainz) because the MB
	// client is built alongside this one; it is only used by the
	// owned-artist backfill, which tolerates its absence.  Guarded by mu.
	mb         *MusicBrainzClient
	artistImg  *ArtistImageProvider
	logger     *slog.Logger
	runtimeCtx context.Context // Wails runtime context for event emission

	cancel context.CancelFunc
	done   chan struct{}

	mu    sync.RWMutex
	ready bool

	// championReady is set once the champion FTS (see
	// RebuildChampionIndex) has been populated; championBuilding guards
	// against overlapping rebuilds.  Both are protected by mu.
	championReady    bool
	championBuilding bool

	// prefixCache memoises the raw index hits for generic short-prefix
	// queries — the expensive ones — so repeats are served from memory.
	prefixCacheMu sync.Mutex
	prefixCache   map[string]prefixCacheEntry

	// discogSF dedupes concurrent lazy discography fetches for the same
	// artist — the detail page fires its top-tracks, top-releases, and
	// similar-artists requests in parallel, so without this the same
	// artist would be fetched several times at once on first view.
	discogSF singleflight.Group

	// Build status tracking — read by GetIndexStatus for the UI.
	buildStatus IndexStatus

	// lastEmitted is the status most recently pushed to the frontend, so
	// an unchanged one can be dropped rather than re-rendering the whole
	// settings page for nothing.  It has its own mutex: emitStatus is
	// called from paths that already hold mu for reading.
	emitMu      sync.Mutex
	lastEmitted *IndexStatus

	// jobs is the background job registry; buildPaused records that the
	// user paused the build, distinguishing a deliberate stop from a
	// build that merely finished.  Both are protected by mu.
	jobs        *jobs.Registry
	buildPaused bool
}

// prefixCacheEntry is one memoised generic-query result.
type prefixCacheEntry struct {
	hits    []SearchIndexResult
	created time.Time
}

// TierStatus represents the state of a single index tier.
type TierStatus struct {
	Name      string `json:"name"`
	State     string `json:"state"` // "pending", "running", "complete", "error", "skipped"
	Total     int    `json:"total"`
	Completed int    `json:"completed"`
	Error     string `json:"error,omitempty"`

	// Detail is a human-readable progress line for stages whose raw
	// completed/total numbers say little on their own — the listens
	// stream reports "42.3 / 205.1 GB · 18 MB/s · ~3h20m left" here.
	Detail string `json:"detail,omitempty"`
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
	//
	// There is deliberately no ticker here.  This used to emit the status
	// every 3 seconds for the life of the process, with a byte-identical
	// payload once the index was ready — which re-rendered the whole of
	// `config-page` on every tick, since it is a cached view that never
	// unmounts.  Every path that mutates the status already calls
	// emitStatus, that call now suppresses an unchanged payload, and the
	// frontend seeds itself with GetIndexStatus() on connect rather than
	// waiting for the next tick.
	si.refreshStatusCounts()
}

// EnsureArtistDiscography lazily fetches an artist's top release groups
// and recordings from ListenBrainz and persists them into the index —
// the first time the artist is actually needed (e.g. their detail page
// opens).  It is a no-op once the artist is marked discog_fetched, so
// each artist costs at most one set of API calls, ever; every later
// view is served from the index with no network.  This replaces the old
// post-scan sweep that eagerly fetched every library artist on startup.
//
// Concurrent calls for the same artist collapse into one via discogSF,
// since the detail page fires several requests for the same artist at
// once.  Safe to call synchronously from a request handler: bounded work
// on its own rate-limited client.
func (si *SearchIndex) EnsureArtistDiscography(ctx context.Context, artistMBID string) {
	if si.lb == nil || artistMBID == "" {
		return
	}

	if si.artistDiscogFetched(artistMBID) {
		return
	}

	_, _, _ = si.discogSF.Do(artistMBID, func() (any, error) {
		// Re-check under the singleflight: a sibling call may have just
		// finished fetching this artist while we were queued.
		if si.artistDiscogFetched(artistMBID) {
			return nil, nil
		}

		indexLB := NewListenBrainzClient(
			NewRateLimiterN(indexerRate), si.lb.cache, si.logger.WithGroup("indexer"),
		)

		si.indexOneArtist(ctx, indexLB, lbSitewideArtist{
			ArtistMBID: artistMBID,
			ArtistName: si.artistDisplayName(artistMBID),
		})

		return nil, nil
	})
}

// artistDiscogFetched reports whether the artist row already has its
// discography fetched, so EnsureArtistDiscography can skip the network.
func (si *SearchIndex) artistDiscogFetched(mbid string) bool {
	rows, err := si.db.QueryContext(
		"SELECT 1 FROM explore_index WHERE entity_type = 1 /* artist */ "+
			"AND mbid = ? AND discog_fetched = 1 LIMIT 1",
		dbMBID(mbid),
	)
	if err != nil {
		return false
	}

	defer func() { _ = rows.Close() }()

	return rows.Next()
}

// unenrichedLibraryArtistMBIDs returns MBIDs for owned artists whose
// discography has not yet been fetched — either they have no index row
// or their row is still discog_fetched = 0, or the full MusicBrainz
// browse has not run.  The LEFT JOINs key off
// persistent marks, so an artist enriched on a prior run (interactively
// or by an earlier backfill) never reappears, giving "new artists only"
// for free.  Ordered by owned-track count so the artists the user has
// most of are enriched first.  The limit bounds a single run (see
// discogBackfillMaxPerRun).
//
// The conditions are OR'd because they are different fetches: an
// artist covered by the downloaded catalog artifact arrives with
// discog_fetched = 1 and has still never been browsed, and the
// artifact's own per-artist coverage is graded — so "the artifact knows
// this artist" is not "we have their discography".
//
// similar_at is deliberately not one of them.  The backfill no longer
// fetches similar artists, so testing the mark it does not set would
// make every owned artist a candidate on every run, forever.
func (si *SearchIndex) unenrichedLibraryArtistMBIDs(limit int) []string {
	rows, err := si.db.QueryContext(`
		SELECT a.mbid
		FROM artists a
		LEFT JOIN explore_index ei
		  ON ei.entity_type = 1 /* artist */ AND ei.mbid = unhex(replace(a.mbid, '-', ''))
		LEFT JOIN artist_enrichment ae ON ae.artist_mbid = a.mbid
		LEFT JOIN audio_files af ON af.artist_id = a.id
		WHERE a.mbid IS NOT NULL AND a.mbid != ''
		  AND (ei.id IS NULL OR ei.discog_fetched = 0
		       OR ae.browsed_at IS NULL)
		GROUP BY a.mbid
		ORDER BY COUNT(DISTINCT af.id) DESC
		LIMIT ?
	`, limit)
	if err != nil {
		si.logger.Warn("discography backfill: query failed", "error", err)

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

// BackfillLibraryDiscographies makes an owned artist's page renderable
// offline, so their catalogue is there right after a scan instead of
// only on first view.  Two fetches per artist, each skipped by its own
// persistent mark:
//
//   - the ListenBrainz top release groups and recordings (marked by
//     explore_index.discog_fetched), which is what popularity ordering
//     and the top-tracks section need;
//   - the full MusicBrainz browse (artist_enrichment.browsed_at), which
//     is what makes the discography complete and typed — see
//     browseFullDiscography.
//
// Similar artists are deliberately *not* fetched here.  Nothing shows
// them until someone opens an artist page, and that page already
// resolves them on demand through Service.SimilarArtists →
// ensureSimilarArtistsAsync, which stamps the same similar_at mark.
// Fetching them for every owned artist bought a third of the run's
// requests for a section most of those artists will never have shown.
//
// It is bounded (discogBackfillMaxPerRun) and resumable: a cancelled or
// capped run continues on the next call, and each artist's marks are
// set as they complete rather than at the end.  Each artist runs
// through discogSF so it never double-fetches one a concurrent
// interactive EnsureArtistDiscography is already handling, and under
// its own deadline so no single artist can stall the run.
func (si *SearchIndex) BackfillLibraryDiscographies(ctx context.Context) {
	if si.lb == nil {
		return
	}

	mbids := si.unenrichedLibraryArtistMBIDs(discogBackfillMaxPerRun)
	if len(mbids) == 0 {
		return
	}

	// Everything below is work nobody is waiting on, so it yields the
	// shared MusicBrainz limiters to whatever the user is looking at.
	ctx = WithBackgroundPriority(ctx)

	job, ctx := startBackfillJob(
		ctx, si.jobRegistry(), discogBackfillJobID,
		"Filling in artist details",
		"Discographies for artists in your library",
		len(mbids),
	)

	defer func() { job.finish(ctx) }()

	// One shared rate limiter paces the whole run, unlike the per-call
	// client EnsureArtistDiscography builds for interactive fetches.
	indexLB := NewListenBrainzClient(
		NewRateLimiterN(indexerRate), si.lb.cache, si.logger.WithGroup("indexer"),
	)

	var (
		wg   sync.WaitGroup
		done atomic.Int64
		work = make(chan string)
	)

	wg.Add(discogBackfillWorkers)

	for range discogBackfillWorkers {
		go func() {
			defer wg.Done()

			for mbid := range work {
				si.backfillOneArtist(ctx, indexLB, mbid)

				job.progress(int(done.Add(1)), len(mbids))
			}
		}()
	}

	for _, mbid := range mbids {
		if ctx.Err() != nil {
			break
		}

		work <- mbid
	}

	close(work)
	wg.Wait()

	total := int(done.Load())

	if ctx.Err() != nil {
		si.logger.Info("discography backfill stopped",
			"artists", total, "of", len(mbids),
		)

		return
	}

	job.logf(jobs.LevelInfo, "Filled in "+strconv.Itoa(total)+" artists")

	si.logger.Info("discography backfill complete", "artists", total)
}

// backfillOneArtist runs one owned artist's fetches, under the
// singleflight that keeps it from racing an interactive fetch and under
// a deadline of its own (see discogBackfillArtistTimeout).
func (si *SearchIndex) backfillOneArtist(
	ctx context.Context, lb *ListenBrainzClient, mbid string,
) {
	if ctx.Err() != nil {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, discogBackfillArtistTimeout)
	defer cancel()

	_, _, _ = si.discogSF.Do(mbid, func() (any, error) {
		// Re-checked under the singleflight: an interactive fetch may
		// have done either of these since the query above.
		if !si.artistDiscogFetched(mbid) {
			si.indexOneArtist(ctx, lb, lbSitewideArtist{
				ArtistMBID: mbid,
				ArtistName: si.artistDisplayName(mbid),
			})
		}

		if !si.enrichmentFor(mbid).Browsed {
			si.browseFullDiscography(ctx, mbid)
		}

		return nil, nil
	})
}

// artistDisplayName resolves a human-readable name for an artist MBID,
// preferring the index title, then the local library, then the MBID
// itself.  Used to seed the discography fetch's artist entry.
func (si *SearchIndex) artistDisplayName(mbid string) string {
	// The two tables spell an MBID differently: the catalog stores raw
	// bytes, the library stores text.  Each query brings its own form.
	for _, q := range []struct {
		sql string
		arg any
	}{
		{"SELECT title FROM explore_index WHERE entity_type = 1 /* artist */ " +
			"AND mbid = ? AND title != '' LIMIT 1", dbMBID(mbid)},
		{"SELECT name FROM artists WHERE mbid = ? AND name != '' LIMIT 1", mbid},
	} {
		rows, err := si.db.QueryContext(q.sql, q.arg)
		if err != nil {
			continue
		}

		if rows.Next() {
			var name string
			if err := rows.Scan(&name); err == nil && name != "" {
				_ = rows.Close()

				return name
			}
		}

		_ = rows.Close()
	}

	return mbid
}

// PersistSimilarArtists stores ListenBrainz similar-artist results into
// the similar_artist_map so future lookups are served locally instead of
// re-hitting the labs API on every artist-page view.
func (si *SearchIndex) PersistSimilarArtists(sourceMBID string, similar []LBSimilarArtist) {
	if len(similar) == 0 {
		return
	}

	wire := make([]lbSimilarArtistWire, len(similar))
	for i, s := range similar {
		wire[i] = lbSimilarArtistWire{
			ArtistMBID: s.ArtistMBID,
			Name:       s.Name,
			Score:      int(s.Score),
		}
	}

	si.storeSimilarArtists(sourceMBID, wire)
}

// StartBuild launches the background index build goroutine.
// Returns immediately.
func (si *SearchIndex) StartBuild(ctx context.Context) {
	// A build the user paused stays paused until they resume it —
	// including across restarts, where the marker is read back from
	// explore_index_meta.  ResumeBuild clears it before calling here.
	if si.buildPausedByUser() {
		si.logger.Info("search index: build is paused, not starting")

		return
	}

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

			// `Building` is derived from si.cancel, so clearing it is a
			// status change and has to say so.  This is what resolves the
			// job in the registry — syncIndexJob only finishes a job on a
			// sync that reports Building false, and without this line the
			// header badge reads "Building search index" over an index the
			// settings page calls ready.
			si.emitStatus()

			close(si.done)

			// The index rows (and their popularities) may have changed, so
			// refresh the champion tier once the build settles.
			si.scheduleChampionRebuild()
		}()

		si.runDumpBuild(buildCtx)
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
			var (
				et    dbEntityType
				count int
			)

			if err := rows.Scan(&et, &count); err == nil {
				switch string(et) {
				case EntityArtist:
					artists = count
				case EntityRecording:
					recordings = count
				case EntityReleaseGroup:
					rgs = count
				}
			}
		}
	}

	var lastBuilt string

	metaRow, err := si.db.QueryContext(
		"SELECT value FROM explore_index_meta WHERE key = 'dump_import_done'",
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
	si.setTierDetail(name, state, total, completed, "")
}

// setTierDetail is setTierStatus plus a human-readable progress line.
func (si *SearchIndex) setTierDetail(name, state string, total, completed int, detail string) {
	si.mu.Lock()

	for i := range si.buildStatus.Tiers {
		if si.buildStatus.Tiers[i].Name == name {
			transitioned := si.buildStatus.Tiers[i].State != state
			si.buildStatus.Tiers[i].State = state
			si.buildStatus.Tiers[i].Total = total
			si.buildStatus.Tiers[i].Completed = completed
			si.buildStatus.Tiers[i].Detail = detail
			si.mu.Unlock()

			if transitioned {
				si.logIndexJob(jobs.LevelInfo, "Stage "+state+": "+name)
			}

			si.emitStatus()

			return
		}
	}

	si.buildStatus.Tiers = append(si.buildStatus.Tiers, TierStatus{
		Name:      name,
		State:     state,
		Total:     total,
		Completed: completed,
		Detail:    detail,
	})

	si.mu.Unlock()
	si.emitStatus()
}

// sameStatusAs reports whether two statuses would render identically.
// IndexStatus holds a slice, so it is not comparable with ==.
func (s IndexStatus) sameStatusAs(o IndexStatus) bool {
	if s.Building != o.Building ||
		s.Ready != o.Ready ||
		s.LastBuilt != o.LastBuilt ||
		s.Artists != o.Artists ||
		s.Recordings != o.Recordings ||
		s.ReleaseGroups != o.ReleaseGroups ||
		s.TotalRows != o.TotalRows ||
		len(s.Tiers) != len(o.Tiers) {
		return false
	}

	for i := range s.Tiers {
		if s.Tiers[i] != o.Tiers[i] {
			return false
		}
	}

	return true
}

// emitStatus pushes the current index status to the frontend via Wails
// event — but only when it differs from the last one pushed.
//
// The status is emitted from every path that touches it, several of
// which report progress in a tight loop, and the frontend's handler
// assigns to a @state field: an identical payload is therefore a full
// re-render of a 2 000-line template saying nothing.  Deduplicating
// here rather than at the call sites means no future emitter has to
// remember (`perf.M6` / `H-14`).
func (si *SearchIndex) emitStatus() {
	if si.runtimeCtx == nil {
		return
	}

	si.mu.RLock()
	status := si.buildStatus
	status.Ready = si.ready
	status.Building = si.cancel != nil
	si.mu.RUnlock()

	si.emitMu.Lock()
	unchanged := si.lastEmitted != nil &&
		si.lastEmitted.sameStatusAs(status)

	if !unchanged {
		snapshot := status
		snapshot.Tiers = append(
			[]TierStatus(nil), status.Tiers...,
		)
		si.lastEmitted = &snapshot
	}
	si.emitMu.Unlock()

	if unchanged {
		return
	}

	events.Emit(si.runtimeCtx, events.IndexStatusChanged, status)

	// Mirror into the shared job registry.  Every status mutation goes
	// through emitStatus, so hooking here covers all update paths.
	si.syncIndexJob(status)
}

// GetPopularity returns the cached popularity (listen count) for
// the given MBID from the local index.  Returns 0 if not found.
func (si *SearchIndex) GetPopularity(mbid string) int {
	if mbid == "" {
		return 0
	}

	rows, err := si.db.QueryContext(
		"SELECT popularity FROM explore_index WHERE mbid = ? LIMIT 1",
		dbMBID(mbid),
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
		args[i] = dbMBID(m)
	}

	query := "SELECT mbid, popularity, listener_count, in_library FROM explore_index WHERE mbid IN (" +
		strings.Join(
			placeholders,
			",",
		) + ")"

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
		var (
			id        dbMBID
			pop       int
			listeners int
			inLib     int
		)

		if err := rows.Scan(&id, &pop, &listeners, &inLib); err == nil {
			mbid := string(id)

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
		dbMBID(mbid),
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
		 WHERE mbid = ? AND entity_type = 1 /* artist */ LIMIT 1`,
		dbMBID(mbid),
	)
	if err != nil {
		return nil
	}

	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return nil
	}

	r := SearchIndexResult{
		EntityType: EntityArtist,
		MBID:       mbid,
	}

	var artist dbMBID

	if err := rows.Scan(
		&r.Title, &r.ArtistName, &artist, &r.Popularity, &r.ListenerCount,
		&r.ArtistType, &r.Country, &r.Disambiguation, &r.SortName, &r.Aliases,
		&r.InLibrary, &r.IsSimilar, &r.LocalArtistID,
	); err != nil {
		return nil
	}

	r.ArtistMBID = string(artist)

	return &r
}

// ReleaseGroupMBIDsForCAAReleaseMBIDs takes a list of release MBIDs
// (from recording.caa_release_mbid) and returns a map from release
// MBID → release group MBID, by joining against the release_group
// rows whose caa_release_mbid matches.  Used to find parent release
// groups for tracks so we can fetch cover art via the existing
// release-group endpoint instead of the per-release endpoint.
func (si *SearchIndex) ReleaseGroupMBIDsForCAAReleaseMBIDs(
	caaReleaseMBIDs []string,
) map[string]string {
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
		args[i] = dbMBID(m)
	}

	query := `SELECT caa_release_mbid, mbid
	          FROM explore_index
	          WHERE entity_type = 2 /* release_group */
	            AND caa_release_mbid != x''
	            AND caa_release_mbid IN (` + strings.Join(placeholders, ",") + `)`

	rows, err := si.db.QueryContext(query, args...)
	if err != nil {
		return nil
	}

	defer func() { _ = rows.Close() }()

	out := make(map[string]string, len(filtered))

	for rows.Next() {
		var caaMBID, rgMBID dbMBID
		if err := rows.Scan(&caaMBID, &rgMBID); err == nil {
			out[string(caaMBID)] = string(rgMBID)
		}
	}

	return out
}

// LookupReleaseGroupByMBID reads a single release group row from the index.
func (si *SearchIndex) LookupReleaseGroupByMBID(mbid string) *SearchIndexResult {
	rows, err := si.db.QueryContext(
		`SELECT title, artist_name, artist_mbid, popularity, listener_count,
		        primary_type, secondary_types, release_date, total_tracks,
		        in_library, COALESCE(local_release_group_id, 0), discog_fetched
		 FROM explore_index
		 WHERE mbid = ? AND entity_type = 2 /* release_group */ LIMIT 1`,
		dbMBID(mbid),
	)
	if err != nil {
		return nil
	}

	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return nil
	}

	r := SearchIndexResult{
		EntityType: EntityReleaseGroup,
		MBID:       mbid,
	}

	var artist dbMBID

	if err := rows.Scan(
		&r.Title, &r.ArtistName, &artist, &r.Popularity, &r.ListenerCount,
		&r.PrimaryType, &r.SecondaryTypes, &r.ReleaseDate, &r.TotalTracks,
		&r.InLibrary, &r.LocalReleaseGroupID, &r.DiscogFetched,
	); err != nil {
		return nil
	}

	return &r
}

// PersistReleaseGroupLookup writes the MB-enriched metadata for a
// single release group back into the index and marks it DiscogFetched
// so LookupReleaseGroup's background enrichment runs at most once per
// RG.  Popularity is left at 0 here — upsertBatch keeps the higher of
// old/new, so the dump-provided popularity is never clobbered.
func (si *SearchIndex) PersistReleaseGroupLookup(rg *MBReleaseGroup) {
	if rg == nil || rg.MBID == "" {
		return
	}

	si.upsertBatch([]SearchIndexResult{{
		EntityType:     "release_group",
		MBID:           rg.MBID,
		Title:          rg.Title,
		ArtistName:     rg.ArtistCredit,
		PrimaryType:    rg.PrimaryType,
		SecondaryTypes: strings.Join(rg.SecondaryTypes, ","),
		ReleaseDate:    rg.FirstReleaseDate,
		DiscogFetched:  true,
	}})
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
		 WHERE artist_mbid = ? AND entity_type = 3 /* recording */
		 ORDER BY popularity DESC
		 LIMIT ?`,
		dbMBID(artistMBID), limit,
	)
	if err != nil {
		return nil
	}

	defer func() { _ = rows.Close() }()

	var results []SearchIndexResult

	for rows.Next() {
		var (
			r       SearchIndexResult
			id, caa dbMBID
		)

		if err := rows.Scan(
			&id, &r.Title, &r.ArtistName, &r.Popularity, &r.ListenerCount,
			&r.Duration, &caa, &r.ReleaseName,
			&r.InLibrary, &r.LocalRecordingID,
		); err == nil {
			r.MBID = string(id)
			r.CAAReleaseMBID = string(caa)
			r.EntityType = EntityRecording
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
		 WHERE artist_mbid = ? AND entity_type = 2 /* release_group */
		 ORDER BY popularity DESC
		 LIMIT ?`,
		dbMBID(artistMBID), limit,
	)
	if err != nil {
		return nil
	}

	defer func() { _ = rows.Close() }()

	var results []SearchIndexResult

	for rows.Next() {
		var (
			r  SearchIndexResult
			id dbMBID
		)

		if err := rows.Scan(
			&id, &r.Title, &r.ArtistName, &r.Popularity, &r.ListenerCount,
			&r.PrimaryType, &r.SecondaryTypes, &r.ReleaseDate,
			&r.InLibrary, &r.LocalReleaseGroupID,
		); err == nil {
			r.MBID = string(id)
			r.EntityType = EntityReleaseGroup
			r.ArtistMBID = artistMBID
			results = append(results, r)
		}
	}

	return results
}

// AddFromCache inserts entries from a cached discography browse into
// the search index.  Called when a user views an artist page and the
// discography is fetched — organic growth beyond the shipped catalog.
func (si *SearchIndex) AddFromCache(artistName, artistMBID string, rgs []MBReleaseGroup) {
	if len(rgs) == 0 {
		return
	}

	// resolveArtistName falls back to the MBID when it cannot find a
	// name, which is fine for a one-off render but must never be
	// persisted: an MBID stored as a title is unsearchable and shows up
	// as a UUID in the UI.  Writing nothing lets the upsert's
	// "non-empty wins" rule keep whatever real name arrives later.
	if artistName == artistMBID {
		artistName = ""
	}

	entries := make([]SearchIndexResult, 0, len(rgs)+1)

	// Add the artist itself, unless there is no name to add.
	if artistName != "" {
		entries = append(entries, SearchIndexResult{
			EntityType: "artist",
			MBID:       artistMBID,
			Title:      artistName,
			ArtistName: artistName,
			ArtistMBID: artistMBID,
			Popularity: 0, // Unknown from this path.
		})
	}

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

	// UNION of two equality lookups rather than `LOWER(title) = ? OR
	// LOWER(artist_name) = ?`.  The OR form forces SQLite to scan all
	// ~240k rows; splitting into two equalities lets each branch seek
	// its partial expression index (idx_explore_title_lower /
	// idx_explore_artist_lower).  Ordering is done in Go below since
	// an ORDER BY here would also defeat the index seek.
	//
	// The popularity clause **must match those indexes' predicate** or
	// the seek becomes a scan of two million rows.  It is the champion
	// set: what the user owns, plus what is popular enough to be worth
	// an exact-match boost.  Anything below the floor is still found by
	// the FTS tiers; it just does not jump the queue.
	rows, err := si.db.QueryContext(`
		SELECT `+indexRowColumns+`
		FROM explore_index
		WHERE LOWER(title) = ? AND (popularity >= ? OR in_library = 1)
		UNION
		SELECT `+indexRowColumns+`
		FROM explore_index
		WHERE LOWER(artist_name) = ? AND (popularity >= ? OR in_library = 1)
	`, q, championPopThreshold, q, championPopThreshold)
	if err != nil {
		return nil
	}

	defer func() { _ = rows.Close() }()

	var matches []SearchIndexResult

	for rows.Next() {
		var r SearchIndexResult

		if err := scanIndexRow(rows, &r); err != nil {
			continue
		}

		// For artists, only match on title (name).  For recordings
		// and release groups, match on either title or artist name
		// — that way "miley cyrus" surfaces both the artist and
		// her recordings.
		titleMatch := strings.ToLower(r.Title) == q
		artistMatch := strings.ToLower(r.ArtistName) == q

		if r.EntityType == "artist" && !titleMatch {
			continue
		}

		if r.EntityType != "artist" && !titleMatch && !artistMatch {
			continue
		}

		matches = append(matches, r)
	}

	// Sort by popularity desc so the per-category cap keeps the most
	// popular entries (the old SQL ORDER BY guaranteed this; UNION
	// does not preserve order).
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].Popularity > matches[j].Popularity
	})

	// Group by entity type, capping at perCategory each.
	buckets := map[string][]SearchIndexResult{
		"artist":        nil,
		"release_group": nil,
		"recording":     nil,
	}

	for _, r := range matches {
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

// Search queries the local FTS5 index and returns matches ordered
// by relevance (popularity-blended).  Returns nil when the index
// hasn't finished its initial build.
func (si *SearchIndex) Search(ctx context.Context, query string, limit int) []SearchIndexResult {
	// IsReady is latched once at startup, so it is right in the app and
	// wrong for anything that fills the table afterwards — including the
	// e2e suite staging a catalog, which is how search gets tested in
	// CI, where the artifact URL points at a dead address on purpose.
	// shelves.go learned this already; the search path did not, and the
	// symptom was three specs that passed only when an earlier one
	// happened to flip the flag first.
	//
	// The latch stays as the fast path — once it is true nothing can
	// make it false — and the probe is one indexed `SELECT 1 … LIMIT 1`
	// on the only path that could otherwise answer "no catalog" wrongly.
	if !si.IsReady() && !si.hasCatalogRows(ctx) {
		return nil
	}

	if limit <= 0 {
		limit = 20
	}

	ftsQuery := buildFTSQuery(query)
	if ftsQuery == "" {
		return nil
	}

	generic := isGenericQuery(query)

	// Generic short-prefix queries are the slow ones; serve repeats from
	// the in-memory cache and route the rest through the champion tier.
	if generic {
		if hits, ok := si.prefixCacheGet(ftsQuery, limit); ok {
			return hits
		}
	}

	hits := si.runSearch(ctx, ftsQuery, limit, generic)

	// Typo-tolerant fallback: a thin result usually means the query is
	// misspelled and matched no token prefix.  Rescue with bigram
	// similarity over the champion set before giving up.  Skipped once a
	// search is superseded (ctx cancelled) — the result is discarded
	// anyway.
	if len(hits) < fuzzyRescueThinHits && ctx.Err() == nil {
		hits = si.appendFuzzyRescue(ctx, query, limit, hits)
	}

	// Cache only fully-formed generic results — never a partial list from
	// a superseded (cancelled) query.
	if generic && ctx.Err() == nil {
		si.prefixCachePut(ftsQuery, limit, hits)
	}

	return hits
}

// appendFuzzyRescue runs the bigram rescue pass and merges its hits into
// the existing (thin) result, de-duplicated by MBID and preserving the
// original hits first.  Returns the original slice unchanged when rescue
// finds nothing new.
func (si *SearchIndex) appendFuzzyRescue(
	ctx context.Context,
	query string,
	limit int,
	hits []SearchIndexResult,
) []SearchIndexResult {
	rescued := si.fuzzyRescue(ctx, query, limit)
	if len(rescued) == 0 {
		return hits
	}

	seen := make(map[string]struct{}, len(hits)+len(rescued))
	for _, h := range hits {
		seen[h.MBID] = struct{}{}
	}

	for _, r := range rescued {
		if _, dup := seen[r.MBID]; dup {
			continue
		}

		seen[r.MBID] = struct{}{}
		hits = append(hits, r)

		if len(hits) >= limit {
			break
		}
	}

	return hits
}

// fuzzyRescue is the typo-tolerant fallback for Search.  It scans the
// champion set (high-popularity + owned rows) and scores each candidate
// by character-bigram overlap (Dice) against its title and artist name,
// so a misspelled query still surfaces its intended entity even though it
// shares no token prefix with any indexed name.  Returns up to `limit`
// hits above fuzzyRescueMinScore, ordered by fuzzy score with popularity
// as a tie-break.  Bounded work that fires only on the rare thin query,
// so it never touches the fast common path.
func (si *SearchIndex) fuzzyRescue(
	ctx context.Context,
	query string,
	limit int,
) []SearchIndexResult {
	qSet := fuzzyBigrams(query)
	if len([]rune(fuzzyNormalize(query))) < fuzzyRescueMinLen || len(qSet) == 0 {
		return nil
	}

	// Champion membership predicate, mirrored from rebuildChampionIndex:
	// a row below the popularity floor that isn't owned can't be a
	// meaningful popular match, so scoring it would only add noise.
	rows, err := si.db.QueryContextWith(ctx, `
		SELECT id, title, artist_name, popularity
		FROM explore_index
		WHERE popularity >= ? OR in_library = 1
	`, championPopThreshold)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			si.logger.Warn("fuzzy rescue scan error", "error", err)
		}

		return nil
	}

	defer func() { _ = rows.Close() }()

	type scored struct {
		id    int64
		score float64
		pop   int
	}

	var candidates []scored

	scanned := 0

	for rows.Next() {
		// Periodically honour cancellation: a superseded search shouldn't
		// keep scoring tens of thousands of rows.
		scanned++
		if scanned%4096 == 0 && ctx.Err() != nil {
			return nil
		}

		var (
			id     int64
			title  string
			artist string
			pop    int
		)

		if err := rows.Scan(&id, &title, &artist, &pop); err != nil {
			continue
		}

		score := diceCoefficient(qSet, fuzzyBigrams(title))
		if s := diceCoefficient(qSet, fuzzyBigrams(artist)); s > score {
			score = s
		}

		if score >= fuzzyRescueMinScore {
			candidates = append(candidates, scored{id: id, score: score, pop: pop})
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	// Best bigram match first; popularity breaks near-ties so the more
	// prominent entity wins when two names are equally close.
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}

		return candidates[i].pop > candidates[j].pop
	})

	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	ids := make([]int64, len(candidates))
	for i, c := range candidates {
		ids[i] = c.id
	}

	return si.rowsByIDs(ctx, ids)
}

// rowsByIDs loads full index rows for the given explore_index ids,
// returned in the same order as `ids` (rows are keyed by id in a map,
// then re-emitted in input order).  Used by fuzzyRescue to hydrate the
// lightweight candidate ids it scored into full search results.
func (si *SearchIndex) rowsByIDs(ctx context.Context, ids []int64) []SearchIndexResult {
	if len(ids) == 0 {
		return nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))

	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := `
		SELECT id, ` + indexRowColumns + `
		FROM explore_index
		WHERE id IN (` + strings.Join(placeholders, ",") + `)`

	rows, err := si.db.QueryContextWith(ctx, query, args...)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			si.logger.Warn("fuzzy rescue hydrate error", "error", err)
		}

		return nil
	}

	defer func() { _ = rows.Close() }()

	byID := make(map[int64]SearchIndexResult, len(ids))

	for rows.Next() {
		var (
			id int64
			r  SearchIndexResult
		)

		if err := scanIndexRow(rows, &r, &id); err != nil {
			continue
		}

		byID[id] = r
	}

	out := make([]SearchIndexResult, 0, len(ids))

	for _, id := range ids {
		if r, ok := byID[id]; ok {
			out = append(out, r)
		}
	}

	return out
}

// runSearch dispatches an FTS query to the champion tier when the query
// is generic and the champion index is ready, falling back to the full
// index when the champion result is thin (few high-popularity matches)
// or the champion index isn't built yet.
func (si *SearchIndex) runSearch(
	ctx context.Context,
	ftsQuery string,
	limit int,
	generic bool,
) []SearchIndexResult {
	if generic {
		if si.championIsReady() {
			cStart := time.Now()
			hits := si.queryFTS(ctx, "explore_champion_fts", ftsQuery, limit)

			if len(hits) >= limit {
				si.logger.Info("search path: champion",
					"fts", ftsQuery,
					"rows", len(hits),
					"elapsed", time.Since(cStart).Round(time.Millisecond),
				)

				return hits
			}

			// Thin champion result: the full index is authoritative, and a
			// query the champion couldn't fill matches few rows there too.
			si.logger.Info("search path: champion thin -> full",
				"fts", ftsQuery,
				"champion_rows", len(hits),
				"champion_elapsed", time.Since(cStart).Round(time.Millisecond),
			)
		} else {
			// First generic query before the champion exists — build it in
			// the background so the next one is fast.
			si.logger.Info("search path: champion not ready -> full", "fts", ftsQuery)
			si.scheduleChampionRebuild()
		}
	}

	fStart := time.Now()
	hits := si.queryFTS(ctx, "explore_index_fts", ftsQuery, limit)

	if generic {
		si.logger.Info("search path: full",
			"fts", ftsQuery,
			"rows", len(hits),
			"elapsed", time.Since(fStart).Round(time.Millisecond),
		)
	}

	return hits
}

// queryFTS runs the popularity-blended ranking query against the named
// FTS table (the full index or the champion tier — identical schema, so
// the only difference is how many rows MATCH).
func (si *SearchIndex) queryFTS(
	ctx context.Context,
	ftsTable, ftsQuery string,
	limit int,
) []SearchIndexResult {
	// ftsTable is a trusted in-package constant, never user input.
	sqlText := fmt.Sprintf(`
		SELECT `+indexRowColumnsFor("i")+`
		FROM explore_index i
		JOIN %[1]s f ON f.rowid = i.id
		WHERE %[1]s MATCH ?
		ORDER BY bm25(%[1]s, 3.0, 1.0, 0.5)
		         - (ln(i.popularity + 1) * 1.5)
		         - (i.in_library * 3.0)
		         - (i.is_similar * 1.5)
		LIMIT ?
	`, ftsTable)

	rows, err := si.db.QueryContextWith(ctx, sqlText, ftsQuery, limit)
	if err != nil {
		// A superseded search has its context cancelled on purpose; that
		// is expected shutdown, not a query fault, so don't warn on it.
		if !errors.Is(err, context.Canceled) {
			si.logger.Warn("search index query error",
				"table", ftsTable,
				"ftsQuery", ftsQuery,
				"error", err,
			)
		}

		return nil
	}

	defer func() { _ = rows.Close() }()

	var results []SearchIndexResult

	for rows.Next() {
		var r SearchIndexResult

		if err := scanIndexRow(rows, &r); err != nil {
			si.logger.Warn("search index scan error", "error", err)

			continue
		}

		results = append(results, r)
	}

	return results
}

// isGenericQuery reports whether a query has no selective (long) token
// and therefore matches a broad slice of the index — the case the
// champion tier and prefix cache exist to accelerate.
func isGenericQuery(query string) bool {
	words := splitWords(query)
	if len(words) == 0 {
		return false
	}

	for _, w := range words {
		if len([]rune(w)) > genericMaxTokenLen {
			return false
		}
	}

	return true
}

// ---------------------------------------------------------------------------
// Champion tier
// ---------------------------------------------------------------------------

// championIsReady reports whether the champion FTS is populated.
func (si *SearchIndex) championIsReady() bool {
	si.mu.RLock()
	defer si.mu.RUnlock()

	return si.championReady
}

// scheduleChampionRebuild rebuilds the champion FTS in the background,
// unless one is already running or the main index isn't ready yet.  The
// rebuild runs in a single write transaction, so concurrent searches
// keep reading the previous champion snapshot (WAL) until it commits.
func (si *SearchIndex) scheduleChampionRebuild() {
	si.mu.Lock()
	if si.championBuilding || !si.ready {
		si.mu.Unlock()

		return
	}

	si.championBuilding = true
	si.mu.Unlock()

	go func() {
		// Use a stable context, not any per-search one, so a superseded
		// search can't cancel the rebuild midway.
		ctx := si.runtimeCtx
		if ctx == nil {
			ctx = context.Background()
		}

		start := time.Now()
		err := si.rebuildChampionIndex(ctx)

		si.mu.Lock()
		si.championBuilding = false
		// Keep a previously-good champion usable if a refresh failed; mark
		// ready on success.
		si.championReady = si.championReady || err == nil
		si.mu.Unlock()

		// The champion set changed, so any memoised generic results are
		// stale.
		si.clearPrefixCache()

		if err != nil {
			si.logger.Warn("champion index rebuild failed", "error", err)

			return
		}

		si.setMeta(championBuiltKey, time.Now().UTC().Format(time.RFC3339))
		si.logger.Info("champion index rebuilt",
			"elapsed", time.Since(start).Round(time.Millisecond),
		)
	}()
}

// rebuildChampionIndex repopulates the champion FTS from the high-
// popularity and owned rows of explore_index.  A row below the
// popularity floor can never place in a generic query's top results
// (ranking there is popularity-dominated), so dropping it is lossless
// for those queries; owned rows are always kept for their in-library
// boost.
func (si *SearchIndex) rebuildChampionIndex(ctx context.Context) error {
	tx, err := si.db.BeginTx()
	if err != nil {
		return fmt.Errorf("begin champion rebuild: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO explore_champion_fts(explore_champion_fts) VALUES('delete-all')`,
	); err != nil {
		return fmt.Errorf("clear champion fts: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO explore_champion_fts(rowid, title, artist_name, aliases)
		SELECT id, title, artist_name, aliases
		FROM explore_index
		WHERE popularity >= ? OR in_library = 1
	`, championPopThreshold); err != nil {
		return fmt.Errorf("populate champion fts: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit champion rebuild: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Prefix result cache
// ---------------------------------------------------------------------------

func prefixCacheKey(ftsQuery string, limit int) string {
	return strconv.Itoa(limit) + "\x00" + ftsQuery
}

func (si *SearchIndex) prefixCacheGet(ftsQuery string, limit int) ([]SearchIndexResult, bool) {
	si.prefixCacheMu.Lock()
	defer si.prefixCacheMu.Unlock()

	key := prefixCacheKey(ftsQuery, limit)

	entry, ok := si.prefixCache[key]
	if !ok {
		return nil, false
	}

	if time.Since(entry.created) > prefixCacheTTL {
		delete(si.prefixCache, key)

		return nil, false
	}

	return entry.hits, true
}

func (si *SearchIndex) prefixCachePut(ftsQuery string, limit int, hits []SearchIndexResult) {
	si.prefixCacheMu.Lock()
	defer si.prefixCacheMu.Unlock()

	if si.prefixCache == nil {
		si.prefixCache = make(map[string]prefixCacheEntry)
	}

	// Generic keys are few and cheap to recompute; when the cap is hit,
	// drop everything rather than track per-entry LRU state.
	if len(si.prefixCache) >= prefixCacheMaxEntries {
		si.prefixCache = make(map[string]prefixCacheEntry)
	}

	si.prefixCache[prefixCacheKey(ftsQuery, limit)] = prefixCacheEntry{
		hits:    hits,
		created: time.Now(),
	}
}

func (si *SearchIndex) clearPrefixCache() {
	si.prefixCacheMu.Lock()
	si.prefixCache = nil
	si.prefixCacheMu.Unlock()
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

type lbSimilarArtistWire struct {
	ArtistMBID    string `json:"artist_mbid"`
	Name          string `json:"name"`
	Score         int    `json:"score"`
	ReferenceMBID string `json:"reference_mbid"` // which seed artist this result belongs to
}

// ---------------------------------------------------------------------------
// Shared: index artist discographies
// ---------------------------------------------------------------------------

func (si *SearchIndex) indexOneArtist(
	ctx context.Context,
	lb *ListenBrainzClient,
	artist lbSitewideArtist,
) {
	if ctx.Err() != nil {
		return
	}

	rgLimit, recLimit := indexMaxRGs, indexMaxRecs

	// Run LB discography fetches and MB artist image resolution
	// concurrently — they use different rate limiters so they
	// don't block each other.
	var (
		rgs    []SearchIndexResult
		recs   []SearchIndexResult
		rgErr  error
		recErr error
		wg     sync.WaitGroup
	)

	// LB pipeline: top release groups + top recordings.
	wg.Add(1)

	go func() {
		defer wg.Done()

		rgs, rgErr = si.fetchTopReleaseGroups(ctx, lb, artist, rgLimit)
		recs, recErr = si.fetchTopRecordings(ctx, lb, artist, recLimit)
	}()

	// MB pipeline: cache the artist lookup, which is what the details
	// below are read from (uses MB rate limiter).
	//
	// Deliberately *not* GetArtistImage.  This function wants the MB
	// artist response; that entry point additionally queried fanart.tv,
	// TheAudioDB, Wikidata and Wikipedia and downloaded up to ten
	// full-size portraits per artist, none of which any caller here
	// reads.  A portrait is resolved when a view asks for one.
	wg.Add(1)

	go func() {
		defer wg.Done()

		if si.artistImg != nil {
			// Carries the caller's priority: interactive from
			// EnsureArtistDiscography, background from the backfill.
			si.artistImg.EnsureArtistRels(ctx, artist.ArtistMBID)
		}
	}()

	wg.Wait()

	// Write the artist entry into the index so indexedArtistMBIDs()
	// recognises this artist as processed on subsequent builds.
	// Also stores aliases and detail fields from the now-cached MB rels
	// (populated by the image resolution above) for FTS search.
	//
	// DiscogFetched records that both LB endpoints were *asked*, not
	// that they had anything to say.  A transient failure still leaves
	// the artist unmarked so the next run retries it — but an artist LB
	// has no popularity data for (or none above indexMinPopularity,
	// which is most of a long-tail library) answers empty every single
	// time, and keying the mark on emptiness made those artists
	// permanent candidates: the owned-artist backfill re-ran for them
	// on every launch, forever, which is the bug this replaces.
	if si.artistImg != nil {
		gotData := rgErr == nil && recErr == nil
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
) ([]SearchIndexResult, error) {
	url := fmt.Sprintf(
		"%s/1/popularity/top-release-groups-for-artist/%s",
		lb.baseURL, artist.ArtistMBID,
	)

	body, err := lb.doGet(ctx, url)
	if err != nil {
		si.logger.Debug("search index: top RGs failed",
			"artist", artist.ArtistName,
			"error", err,
		)

		return nil, err
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
		return nil, err
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

	return results, nil
}

func (si *SearchIndex) fetchTopRecordings(
	ctx context.Context,
	lb *ListenBrainzClient,
	artist lbSitewideArtist,
	maxCount int,
) ([]SearchIndexResult, error) {
	url := fmt.Sprintf(
		"%s/1/popularity/top-recordings-for-artist/%s",
		lb.baseURL, artist.ArtistMBID,
	)

	body, err := lb.doGet(ctx, url)
	if err != nil {
		return nil, err
	}

	var raw []lbTopRecordingWire
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
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

	return results, nil
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

// indexRowColumns is the full explore_index projection, and
// scanIndexRow is the only thing that reads it.
//
// There were four copies of this column list and four matching Scan
// calls, which is what makes the storage encoding dangerous: a blob
// column scanned into a string yields sixteen bytes of garbage rather
// than an error, and it would have had to be got right four times.
// dbMBID and dbEntityType do the decoding, and they refuse anything
// that is not what they expect.
var indexRowFields = []string{
	"entity_type", "mbid", "title", "artist_name", "artist_mbid",
	"popularity", "listener_count", "duration", "primary_type",
	"secondary_types", "release_date", "total_tracks", "caa_release_mbid",
	"release_name", "artist_type", "country", "disambiguation",
	"sort_name", "in_library", "is_similar",
	"local_artist_id", "local_release_group_id", "local_recording_id",
}

// nullableIndexRowFields are the ones a caller wants zero rather than
// NULL for.
var nullableIndexRowFields = map[string]bool{
	"local_artist_id":        true,
	"local_release_group_id": true,
	"local_recording_id":     true,
}

// indexRowColumns is the projection unqualified; indexRowColumnsFor
// qualifies it with a table alias, for the joins where the other side
// also has a `title`.
var indexRowColumns = indexRowColumnsFor("")

func indexRowColumnsFor(alias string) string {
	if alias != "" {
		alias += "."
	}

	cols := make([]string, 0, len(indexRowFields))

	for _, f := range indexRowFields {
		if nullableIndexRowFields[f] {
			cols = append(cols, "COALESCE("+alias+f+", 0)")

			continue
		}

		cols = append(cols, alias+f)
	}

	return strings.Join(cols, ", ")
}

// scanIndexRow reads one indexRowColumns row into a result.
func scanIndexRow(rows *sql.Rows, r *SearchIndexResult, before ...any) error {
	var (
		entity dbEntityType
		id     dbMBID
		artist dbMBID
		caa    dbMBID
	)

	dest := make([]any, 0, len(before)+len(indexRowFields))
	dest = append(dest, before...)
	dest = append(dest,
		&entity, &id, &r.Title, &r.ArtistName, &artist,
		&r.Popularity, &r.ListenerCount, &r.Duration, &r.PrimaryType,
		&r.SecondaryTypes, &r.ReleaseDate, &r.TotalTracks, &caa,
		&r.ReleaseName, &r.ArtistType, &r.Country, &r.Disambiguation,
		&r.SortName, &r.InLibrary, &r.IsSimilar,
		&r.LocalArtistID, &r.LocalReleaseGroupID, &r.LocalRecordingID,
	)

	if err := rows.Scan(dest...); err != nil {
		return fmt.Errorf("scan index row: %w", err)
	}

	r.EntityType = string(entity)
	r.MBID = string(id)
	r.ArtistMBID = string(artist)
	r.CAAReleaseMBID = string(caa)

	return nil
}

// upsertIndexSQL is the single index write statement.  It is kept as
// a const so assembly can prepare it once per transaction instead of
// re-parsing this large upsert for every row.
const upsertIndexSQL = `
	INSERT INTO explore_index (
		entity_type, mbid, title, artist_name, artist_mbid, aliases,
		popularity, listener_count,
		duration, caa_release_mbid, release_name,
		primary_type, secondary_types, release_date, total_tracks,
		artist_type, country, disambiguation, sort_name,
		in_library, is_similar,
		local_artist_id, local_release_group_id, local_recording_id,
		discog_fetched
	) VALUES (
		?, ?, ?, ?, ?, ?,
		?, ?,
		?, ?, ?,
		?, ?, ?, ?,
		?, ?, ?, ?,
		?, ?,
		NULLIF(?, 0), NULLIF(?, 0), NULLIF(?, 0),
		?
	)` + upsertIndexConflictSQL

// upsertIndexConflictSQL is the merge half of every index write, split
// out so the bulk artifact import (which inserts by SELECT rather than
// by parameter list) resolves conflicts identically instead of carrying
// a second, drifting copy of these rules.
const upsertIndexConflictSQL = `
	ON CONFLICT(mbid) DO UPDATE SET
		-- Title and artist info: never clobber a good value with an
		-- empty one.  Writers are responsible for not offering an MBID
		-- as a name; AddFromCache is the path that used to.
		title       = CASE WHEN excluded.title != '' THEN excluded.title ELSE title END,
		artist_name = CASE WHEN excluded.artist_name != '' THEN excluded.artist_name ELSE artist_name END,
		artist_mbid = CASE WHEN excluded.artist_mbid != x'' THEN excluded.artist_mbid ELSE artist_mbid END,
		aliases     = CASE WHEN excluded.aliases != '' THEN excluded.aliases ELSE aliases END,

		-- Highest wins for popularity + listener_count (refreshes can go up).
		popularity     = CASE WHEN excluded.popularity > popularity THEN excluded.popularity ELSE popularity END,
		listener_count = CASE WHEN excluded.listener_count > listener_count THEN excluded.listener_count ELSE listener_count END,

		-- Non-empty wins for all other optional fields (never clobber with empty).
		duration         = CASE WHEN excluded.duration > 0 THEN excluded.duration ELSE duration END,
		caa_release_mbid = CASE WHEN excluded.caa_release_mbid != x'' THEN excluded.caa_release_mbid ELSE caa_release_mbid END,
		release_name     = CASE WHEN excluded.release_name != '' THEN excluded.release_name ELSE release_name END,
		primary_type     = CASE WHEN excluded.primary_type != '' THEN excluded.primary_type ELSE primary_type END,
		secondary_types  = CASE WHEN excluded.secondary_types != '' THEN excluded.secondary_types ELSE secondary_types END,
		release_date     = CASE WHEN excluded.release_date != '' THEN excluded.release_date ELSE release_date END,
		total_tracks     = CASE WHEN excluded.total_tracks > 0 THEN excluded.total_tracks ELSE total_tracks END,
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
		local_recording_id     = COALESCE(excluded.local_recording_id, local_recording_id)
`

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

	stmt, err := tx.Prepare(upsertIndexSQL)
	if err != nil {
		si.logger.Warn("search index: prepare upsert error", "error", err)

		_ = tx.Rollback()

		return
	}

	defer func() { _ = stmt.Close() }()

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

		if _, err := stmt.Exec(
			dbEntityType(e.EntityType), dbMBID(e.MBID),
			e.Title, e.ArtistName, dbMBID(e.ArtistMBID), e.Aliases,
			e.Popularity, e.ListenerCount,
			e.Duration, dbMBID(e.CAAReleaseMBID), e.ReleaseName,
			e.PrimaryType, e.SecondaryTypes, e.ReleaseDate, e.TotalTracks,
			e.ArtistType, e.Country, e.Disambiguation, e.SortName,
			inLib, isSim,
			e.LocalArtistID, e.LocalReleaseGroupID, e.LocalRecordingID,
			discogFetched,
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

// hasMeta reports whether a key exists in explore_index_meta.
func (si *SearchIndex) hasMeta(key string) bool {
	rows, err := si.db.QueryContext(
		"SELECT 1 FROM explore_index_meta WHERE key = ?", key,
	)
	if err != nil {
		return false
	}

	defer func() { _ = rows.Close() }()

	return rows.Next()
}

// PopulateLocalCrossReferences syncs the local library into the search
// index: every MB-verified library artist, release group, and recording
// is upserted into explore_index, flagged in_library with its local id.
// Call after a library scan completes.
//
// This is the local half of the index — it makes owned content
// searchable regardless of the dump's popularity floor, using only data
// already in the library tables (no API calls).  upsertBatch's conflict
// rules mean dump-seeded rows are merely tagged in_library while
// below-floor owned entities are inserted fresh (popularity 0, to be
// filled later by incremental dumps or a lazy discography fetch).
// MBID-less local content is intentionally excluded — the explore index
// is MBID-keyed, and unmatched files are served by the library search.
func (si *SearchIndex) PopulateLocalCrossReferences() {
	// Clear cross-references for entities no longer owned before adding
	// current ones — the upsert below only ever adds/refreshes rows for
	// what's currently in the library, so removals need their own pass.
	si.pruneStaleLocalCrossReferences()

	entries := si.collectLibraryEntities()
	if len(entries) == 0 {
		si.logger.Info("library sync: no MB-verified library entities to index")
		si.setMeta(localXrefReadyKey, "1")

		return
	}

	const batchSize = 1000
	for i := 0; i < len(entries); i += batchSize {
		end := i + batchSize
		if end > len(entries) {
			end = len(entries)
		}

		si.upsertBatch(entries[i:end])
	}

	si.setMeta(localXrefReadyKey, "1")
	si.logger.Info("library sync: upserted library entities into index", "count", len(entries))
}

// pruneStaleLocalCrossReferences clears in_library/local_*_id on
// explore_index rows whose local row no longer exists — e.g. an artist
// whose owned files were swapped out and removed by a rescan.  The
// index upsert (upsertIndexConflictSQL) is a one-way ratchet that only
// ever sets these columns, never clears them, so this is the only place
// a removal from the library is ever reflected back into the index.
// The row itself is left in place (it may still be part of the shipped
// catalog, just no longer owned) — only the "this is mine" bookkeeping
// is cleared.
func (si *SearchIndex) pruneStaleLocalCrossReferences() {
	type prune struct {
		entityType string
		column     string
		// exists is the test for "this local id still refers to
		// something the user owns".  It is a file test in every case -
		// the version that tested the metadata table left 129 rows in
		// a real catalog claiming to be owned by files that were gone.
		exists string
	}

	for _, p := range []prune{
		{"artist", "local_artist_id", `
			SELECT 1 FROM artists a WHERE a.id = explore_index.local_artist_id
			AND (
				EXISTS (SELECT 1 FROM audio_files af WHERE af.artist_id = a.id)
				OR EXISTS (
					SELECT 1 FROM albums al
					JOIN audio_files af2 ON af2.album_id = al.id
					WHERE al.artist_id = a.id
				)
			)`},
		{"release_group", "local_release_group_id", `
			SELECT 1 FROM audio_files af
			WHERE af.album_id = explore_index.local_release_group_id`},
		{"recording", "local_recording_id", `
			SELECT 1 FROM audio_files af
			WHERE af.id = explore_index.local_recording_id`},
	} {
		result, err := si.db.ExecContext(
			`UPDATE explore_index
			 SET in_library = 0, `+p.column+` = NULL
			 WHERE entity_type = ? AND `+p.column+` IS NOT NULL
			   AND NOT EXISTS (`+p.exists+`)`,
			dbEntityType(p.entityType),
		)
		if err != nil {
			si.logger.Warn("library sync: prune stale cross-references failed",
				"entityType", p.entityType, "error", err)

			continue
		}

		if n, _ := result.RowsAffected(); n > 0 {
			si.logger.Info("library sync: cleared stale cross-references",
				"entityType", p.entityType, "count", n)
		}
	}
}

// collectLibraryEntities builds index entries for every library artist,
// release group, and recording that carries a MusicBrainz ID.  Artist
// credit strings and the primary artist MBID are resolved from the local
// artist_credit tables so no network lookup is needed.
func (si *SearchIndex) collectLibraryEntities() []SearchIndexResult {
	var entries []SearchIndexResult

	// Every one of these is gated on a file existing.  They used to
	// select straight from the metadata tables, so an artist, album or
	// recording whose files were gone stayed flagged "in library" in
	// the catalog until something noticed - and nothing did.
	type entityQuery struct {
		kind  string
		query string
	}

	queries := []entityQuery{
		{"artist", `
			SELECT DISTINCT a.id, a.name, a.mbid, a.name, a.mbid
			FROM artists a
			WHERE a.mbid IS NOT NULL AND a.mbid != ''
			  AND (
				EXISTS (SELECT 1 FROM audio_files af WHERE af.artist_id = a.id)
				OR EXISTS (
					SELECT 1 FROM albums al
					JOIN audio_files af2 ON af2.album_id = al.id
					WHERE al.artist_id = a.id
				)
			  )`},
		{"release_group", `
			SELECT DISTINCT al.id, al.name, al.mbid, al.artist_credit,
			       COALESCE(ar.mbid, '')
			FROM albums al
			JOIN audio_files af ON af.album_id = al.id
			LEFT JOIN artists ar ON ar.id = al.artist_id
			WHERE al.mbid IS NOT NULL AND al.mbid != ''`},
		{"recording", `
			SELECT af.id, af.title, af.recording_mbid, af.artist_credit,
			       COALESCE(ar.mbid, '')
			FROM audio_files af
			LEFT JOIN artists ar ON ar.id = af.artist_id
			WHERE af.recording_mbid IS NOT NULL AND af.recording_mbid != ''`},
	}

	for _, eq := range queries {
		rows, err := si.db.QueryContext(eq.query)
		if err != nil {
			si.logger.Warn("library sync: query failed", "kind", eq.kind, "error", err)

			continue
		}

		for rows.Next() {
			var (
				id                           int64
				name, mbid, credit, artistMB string
			)

			if err := rows.Scan(&id, &name, &mbid, &credit, &artistMB); err != nil {
				continue
			}

			entry := SearchIndexResult{
				EntityType: eq.kind,
				MBID:       mbid,
				Title:      name,
				ArtistName: credit,
				ArtistMBID: artistMB,
				InLibrary:  true,
			}

			switch eq.kind {
			case "artist":
				entry.LocalArtistID = id
			case "release_group":
				entry.LocalReleaseGroupID = id
			case "recording":
				// The local id of a "recording" is the file's, which is
				// what every caller wants: it is the thing that plays.
				entry.LocalRecordingID = id
			}

			entries = append(entries, entry)
		}

		_ = rows.Close()
	}

	return entries
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
			dbMBID(mbid),
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
		strings.Join(
			placeholders,
			",",
		) + ") GROUP BY similar_artist_mbid"

	rows, err := si.db.QueryContext(query, args...)
	if err != nil {
		return nil
	}

	defer func() { _ = rows.Close() }()

	result := make(map[string]int, len(mbids))

	for rows.Next() {
		var (
			mbid  string
			score int
		)

		if err := rows.Scan(&mbid, &score); err == nil {
			result[mbid] = score
		}
	}

	return result
}

// InvalidateDiscographies clears the dump-import completion marker so
// the next StartBuild re-runs the full dump import.
func (si *SearchIndex) InvalidateDiscographies() {
	_, _ = si.db.ExecContext(
		"DELETE FROM explore_index_meta WHERE key = 'dump_import_done'",
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

// deleteMeta removes a key from explore_index_meta, e.g. to invalidate a
// "ready" marker so the next launch re-runs a gated build step.
func (si *SearchIndex) deleteMeta(key string) {
	if _, err := si.db.ExecContext(
		"DELETE FROM explore_index_meta WHERE key = ?", key,
	); err != nil {
		si.logger.Warn("search index: delete meta error", "key", key, "error", err)
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

	var count int
	if rows.Next() {
		_ = rows.Scan(&count)
	}
	// Release the connection before any further query: under a
	// single-connection pool (tests) a nested query while these rows are
	// open would deadlock.
	_ = rows.Close()

	if count == 0 {
		return
	}

	// Trust a champion index built in a previous run; otherwise build it
	// below now that the main index is known-ready.
	championBuilt := si.hasMeta(championBuiltKey)

	si.mu.Lock()
	si.ready = true
	si.championReady = championBuilt
	si.mu.Unlock()

	si.logger.Info("search index: using existing index", "entries", count)

	// Becoming ready is a change the UI has to see, and this was the one
	// path that mutated the status without saying so — the 3 s ticker
	// carried it, invisibly, which is why removing the ticker without
	// this line would have left the settings page reading "not ready"
	// over a fully built index.
	si.emitStatus()

	if !championBuilt {
		si.scheduleChampionRebuild()
	}
}
