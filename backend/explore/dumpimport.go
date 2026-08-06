//go:build indexbuild

package explore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"sync/atomic"
	"time"

	"yellowjacket/backend/database"
	"yellowjacket/backend/jobs"
	"yellowjacket/backend/system"
)

// Dump-based index population.  Instead of crawling the ListenBrainz
// API artist-by-artist, the index is built from two MetaBrainz dumps:
//
//  1. The spark listens dump (~205GB on the server, of which only the
//     three MBID columns are downloaded — see dumpproject.go) yields
//     listen counts for every recording/release/artist MBID.
//  2. The MusicBrainz canonical dump (~2GB, streamed) yields names and
//     MBIDs, filtered to entities above a popularity floor.
//
// A short API patch pass then fills listener counts (top rows only),
// artist metadata, and the similar-artist map.  Total temporary disk
// is one ~1GB counts file, deleted on completion.  The pipeline
// resumes from checkpoints after interruption.

const (

	// releaseToRGInsertBatch bounds how many rows are written per
	// transaction when persisting the release→release-group map.
	releaseToRGInsertBatch = 10_000

	// dumpMinStartFreeBytes is the free-disk requirement to begin an
	// import (counts file + index growth + headroom).
	dumpMinStartFreeBytes = 6 << 30

	// dumpAbortFreeBytes aborts a running import when free disk
	// drops below it.
	dumpAbortFreeBytes = 2 << 30

	// dumpStageAssembled in state.json means the index rows are
	// written and only patch passes remain.
	dumpStageAssembled = "assembled"
)

// Dump import stages, mapped to status names shown in the UI.
const (
	dumpStageCounts = iota
	dumpStageCatalog
	dumpStagePatch
	dumpStageListeners
)

var dumpStageNames = [...]string{
	"Listen Counts",
	"Catalog Import",
	"Metadata Patch",
	"Listener Counts",
}

// Dump discovery patterns.
var (
	canonicalDirRe  = regexp.MustCompile(`^musicbrainz-canonical-dump-\d{8}-\d+$`)
	canonicalFileRe = regexp.MustCompile(`^musicbrainz-canonical-dump-.*\.tar\.zst$`)
	listensDirRe    = regexp.MustCompile(`^listenbrainz-dump-\d+-\d{8}-\d+-full$`)
	sparkFileRe     = regexp.MustCompile(`^listenbrainz-spark-dump-.*-full\.tar$`)
)

// Production dump locations (overridable for tests).
const (
	defaultCanonicalBaseURL = "https://data.metabrainz.org/pub/musicbrainz/canonical_data/"
	defaultListensBaseURL   = "https://data.metabrainz.org/pub/musicbrainz/listenbrainz/fullexport/"
)

// dumpImportState is the small persistent state file (staging dir).
// The heavyweight stage-1 checkpoint lives in counts.bin.
type dumpImportState struct {
	SparkURL     string `json:"sparkUrl"`
	CanonicalURL string `json:"canonicalUrl"`
	Stage        string `json:"stage"`
}

// dumpImporter runs the dump import pipeline.
type dumpImporter struct {
	si     *SearchIndex
	lb     *ListenBrainzClient
	logger *slog.Logger

	httpClient *http.Client
	stagingDir string

	canonicalBaseURL string
	listensBaseURL   string

	// Disk safety floors (fields so tests can relax them).
	minStartFreeBytes uint64
	abortFreeBytes    uint64

	// pendingArtists are kept artists whose names weren't derivable
	// from the canonical dump; the metadata patch pass resolves them.
	pendingArtists []string

	// countsRate is the listens stream throughput in bytes/sec, written
	// by the stage-1 progress reporter and read by its loggers.
	countsRate atomic.Uint64

	// ftsResumed guards the bulk-load window so the FTS rebuild runs
	// exactly once per import.
	ftsResumed bool
}

func newDumpImporter(si *SearchIndex, lb *ListenBrainzClient) (*dumpImporter, error) {
	dataDir, err := system.GetUserDataDirPath()
	if err != nil {
		return nil, fmt.Errorf("dump import: data dir: %w", err)
	}

	stagingDir := filepath.Join(dataDir, "explore-staging")
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return nil, fmt.Errorf("dump import: staging dir: %w", err)
	}

	return &dumpImporter{
		si:     si,
		lb:     lb,
		logger: si.logger,
		// HTTP/1.1-only, no client-level timeout: the listens stream
		// runs for hours.  Discovery requests use per-request context
		// timeouts, and the stream readers recover from stalls.
		httpClient:        newDumpHTTPClient(),
		stagingDir:        stagingDir,
		canonicalBaseURL:  defaultCanonicalBaseURL,
		listensBaseURL:    defaultListensBaseURL,
		minStartFreeBytes: dumpMinStartFreeBytes,
		abortFreeBytes:    dumpAbortFreeBytes,
	}, nil
}

func (imp *dumpImporter) countsPath() string {
	return filepath.Join(imp.stagingDir, "counts.bin")
}

func (imp *dumpImporter) statePath() string {
	return filepath.Join(imp.stagingDir, "state.json")
}

// run executes the pipeline, resuming from any prior checkpoint.
func (imp *dumpImporter) run(ctx context.Context) error {
	if err := checkFreeDisk(imp.stagingDir, imp.minStartFreeBytes); err != nil {
		return err
	}

	state, err := imp.readState()
	if err != nil {
		return err
	}

	// Fast path: rows already assembled, only patch passes remain.
	if state.Stage == dumpStageAssembled {
		imp.si.MarkReadyIfPopulated()
		imp.runPatchPasses(ctx)

		if err := ctx.Err(); err != nil {
			return err
		}

		return imp.finalize()
	}

	// Stage 1: listen counts from the spark listens dump.
	counts, err := imp.readCountsFile()
	if err != nil {
		imp.logger.Warn("dump import: discarding unreadable counts checkpoint", "error", err)

		counts = nil
	}

	if counts == nil {
		sparkURL, err := discoverDumpFile(
			ctx, imp.httpClient, imp.listensBaseURL, listensDirRe, sparkFileRe,
		)
		if err != nil {
			return err
		}

		imp.logger.Info("dump import: starting", "listensDump", sparkURL)
		imp.logJob("Streaming listens dump " + path.Base(sparkURL) +
			" — this stage reads tens of gigabytes and runs for hours")

		counts = &countsState{SparkURL: sparkURL}
	} else if !counts.Done {
		imp.logger.Info("dump import: resuming listen counts",
			"offset", counts.Offset,
			"members", counts.MemberIdx,
			"entities", len(counts.counts),
		)
		imp.logJob(fmt.Sprintf(
			"Resuming listen counts from %s (%s members, %s entities so far)",
			formatGB(counts.Offset), formatCount(counts.MemberIdx), formatCount(len(counts.counts)),
		))
	}

	// Record which dump series this import is baselined on, so the
	// incremental refresh knows where to resume applying daily deltas.
	// Written early (before assembly) so it survives a crash-and-resume;
	// incrementals only apply once dumpImportDoneKey confirms completion.
	imp.recordDumpSeries(counts.SparkURL)

	imp.setStageProgress(dumpStageCounts, 0, 100)

	if !counts.Done {
		if err := imp.aggregateListenCounts(ctx, counts); err != nil {
			return err
		}
	}

	imp.si.setTierStatus(dumpStageNames[dumpStageCounts], "complete", 100, 100)

	// Stage 2: popularity thresholds (in RAM, deterministic).
	kept := imp.computeThreshold(counts.counts)

	// Free the full counts map; only the kept sets are needed now.
	counts.counts = nil

	// Stage 3: canonical dump scan + assembly.  Restartable: the
	// scan is a cheap 2GB stream and assembly is an idempotent
	// upsert, so no intra-stage checkpoint is needed.
	if state.CanonicalURL == "" {
		state.CanonicalURL, err = discoverDumpFile(
			ctx, imp.httpClient, imp.canonicalBaseURL, canonicalDirRe, canonicalFileRe,
		)
		if err != nil {
			return err
		}

		if err := imp.writeState(state); err != nil {
			return err
		}
	}

	imp.setStageProgress(dumpStageCatalog, 0, 0)

	scan, err := imp.scanCanonicalDump(ctx, state.CanonicalURL, kept)
	if err != nil {
		return err
	}

	if err := imp.checkDiskHeadroom(); err != nil {
		return err
	}

	// Bulk-load window: the wipe below and the millions of upserts that
	// follow would otherwise each maintain the FTS5 index row by row,
	// which dominates the entire import (~31 rows/s vs ~4,700).  The
	// index is rebuilt in one pass when the window closes.
	if err := imp.si.db.SuspendExploreIndexFTS(); err != nil {
		// Not fatal: the import still completes, just slowly.
		imp.logger.Warn("dump import: could not suspend FTS sync", "error", err)
	} else {
		defer imp.resumeFTS()
	}

	if err := imp.assembleIndex(ctx, kept, scan); err != nil {
		return err
	}

	// Persist the release→release-group map (otherwise in-memory only)
	// so incremental dumps can roll per-release listen deltas up to their
	// album without an API call.  Kept in lockstep with the index it was
	// just built from.
	imp.persistReleaseToRG(ctx, scan.releaseToRG)

	// Close the bulk-load window now rather than at return: the patch
	// passes below run at API rate, so per-row FTS upkeep costs nothing
	// there and keeps search current while they work.
	imp.resumeFTS()

	imp.si.setTierStatus(dumpStageNames[dumpStageCatalog], "complete", 0, 0)

	// Artists that need names from the metadata patch pass.
	for mbid := range kept.artists {
		if _, ok := scan.artistNames[mbid]; !ok {
			imp.pendingArtists = append(imp.pendingArtists, formatUUID(mbid[:]))
		}
	}

	state.Stage = dumpStageAssembled
	if err := imp.writeState(state); err != nil {
		return err
	}

	imp.si.MarkReadyIfPopulated()
	imp.si.refreshStatusCounts()

	// Stage 4: API patch passes (idempotent).
	imp.runPatchPasses(ctx)

	if err := ctx.Err(); err != nil {
		return err
	}

	return imp.finalize()
}

// resumeFTS closes the bulk-load window, restoring the FTS sync
// triggers and rebuilding the index.  Idempotent, so it can run both at
// its natural point in the pipeline and from a defer covering the
// error and cancellation paths.
func (imp *dumpImporter) resumeFTS() {
	if imp.ftsResumed {
		return
	}

	imp.ftsResumed = true

	start := time.Now()

	if err := imp.si.db.ResumeExploreIndexFTS(); err != nil {
		// Leaving search unindexed is worse than a slow import, so this
		// is loud: it needs a rebuild to recover.
		imp.logger.Error("dump import: FTS rebuild failed — search index is stale",
			"error", err,
		)

		return
	}

	imp.logger.Info("dump import: FTS index rebuilt",
		"elapsed", time.Since(start).Round(time.Millisecond),
	)
}

// finalize records completion and removes all staging data.
func (imp *dumpImporter) finalize() error {
	imp.si.setMeta(dumpImportDoneKey, time.Now().UTC().Format(time.RFC3339))

	if err := os.RemoveAll(imp.stagingDir); err != nil {
		imp.logger.Warn("dump import: staging cleanup failed", "error", err)
	}

	imp.si.setTierStatus(dumpStageNames[dumpStagePatch], "complete", 0, 0)
	imp.si.setTierStatus(dumpStageNames[dumpStageListeners], "complete", 0, 0)
	imp.si.refreshStatusCounts()

	// The imported catalog changed which rows are popular, so refresh the
	// champion tier used for generic short-prefix searches.
	imp.si.scheduleChampionRebuild()

	imp.logger.Info("dump import: complete")

	return nil
}

// recordDumpSeries stores the baseline series number for this import.
func (imp *dumpImporter) recordDumpSeries(sparkURL string) {
	series, ok := parseDumpSeries(sparkURL)
	if !ok {
		imp.logger.Warn("dump import: could not parse dump series", "url", sparkURL)

		return
	}

	imp.si.setMeta(listensAppliedSeriesKey, strconv.Itoa(series))
}

// persistReleaseToRG replaces the release_to_rg table with the mapping
// captured during this import, so it always reflects the just-built
// index.  Idempotent: a full rebuild clears and repopulates it.
func (imp *dumpImporter) persistReleaseToRG(ctx context.Context, m map[uuid16]rgTarget) {
	if len(m) == 0 {
		return
	}

	if _, err := imp.si.db.ExecContext("DELETE FROM release_to_rg"); err != nil {
		imp.logger.Warn("dump import: clear release_to_rg failed", "error", err)

		return
	}

	written := 0
	pending := 0

	// The statement is prepared per transaction rather than passed to
	// tx.Exec per row: re-parsing it for each of several million rows
	// costs an order of magnitude more than the insert itself.
	tx, stmt, err := beginReleaseToRGTx(imp.si.db)
	if err != nil {
		imp.logger.Warn("dump import: begin release_to_rg tx failed", "error", err)

		return
	}

	for rel, target := range m {
		if ctx.Err() != nil {
			_ = stmt.Close()
			_ = tx.Rollback()

			return
		}

		if _, err := stmt.Exec(
			formatUUID(rel[:]), formatUUID(target.rg[:]),
		); err != nil {
			imp.logger.Warn("dump import: insert release_to_rg failed", "error", err)

			continue
		}

		written++
		pending++

		if pending >= releaseToRGInsertBatch {
			_ = stmt.Close()

			if err := tx.Commit(); err != nil {
				imp.logger.Warn("dump import: commit release_to_rg batch failed", "error", err)

				return
			}

			pending = 0

			tx, stmt, err = beginReleaseToRGTx(imp.si.db)
			if err != nil {
				imp.logger.Warn("dump import: begin release_to_rg tx failed", "error", err)

				return
			}
		}
	}

	_ = stmt.Close()

	if err := tx.Commit(); err != nil {
		imp.logger.Warn("dump import: commit release_to_rg failed", "error", err)

		return
	}

	imp.logger.Info("dump import: persisted release→release-group map", "rows", written)
}

// beginReleaseToRGTx opens a batch transaction with the row insert
// already prepared on it.
func beginReleaseToRGTx(db *database.DB) (*sql.Tx, *sql.Stmt, error) {
	tx, err := db.BeginTx()
	if err != nil {
		return nil, nil, fmt.Errorf("release_to_rg begin: %w", err)
	}

	stmt, err := tx.Prepare(
		"INSERT OR REPLACE INTO release_to_rg (release_mbid, rg_mbid) VALUES (?, ?)",
	)
	if err != nil {
		_ = tx.Rollback()

		return nil, nil, fmt.Errorf("release_to_rg prepare: %w", err)
	}

	return tx, stmt, nil
}

func (imp *dumpImporter) readState() (*dumpImportState, error) {
	state := &dumpImportState{}

	data, err := os.ReadFile(imp.statePath())
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}

	if err != nil {
		return nil, fmt.Errorf("dump import state read: %w", err)
	}

	if err := json.Unmarshal(data, state); err != nil {
		// Corrupt state: start over rather than fail permanently.
		return &dumpImportState{}, nil
	}

	return state, nil
}

func (imp *dumpImporter) writeState(state *dumpImportState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("dump import state marshal: %w", err)
	}

	tmp := imp.statePath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("dump import state write: %w", err)
	}

	if err := os.Rename(tmp, imp.statePath()); err != nil {
		return fmt.Errorf("dump import state rename: %w", err)
	}

	return nil
}

// setStageProgress reports stage progress to the UI status feed.
func (imp *dumpImporter) setStageProgress(stage, completed, total int) {
	imp.si.setTierStatus(dumpStageNames[stage], "running", total, completed)
}

// setStageDetail is setStageProgress plus a human-readable line for
// stages where completed/total alone is uninformative.
func (imp *dumpImporter) setStageDetail(stage, completed, total int, detail string) {
	imp.si.setTierDetail(dumpStageNames[stage], "running", total, completed, detail)
}

// logJob appends a line to the index build's job log, which is what the
// user sees in the jobs panel.  A build with no registered job (tests,
// headless imports) drops the line.
func (imp *dumpImporter) logJob(message string) {
	imp.si.logIndexJob(jobs.LevelInfo, message)
}

// checkDiskHeadroom aborts the import when free disk is critically low.
func (imp *dumpImporter) checkDiskHeadroom() error {
	return checkFreeDisk(imp.stagingDir, imp.abortFreeBytes)
}

// ---------------------------------------------------------------------------
// SearchIndex integration
// ---------------------------------------------------------------------------

// runDumpBuild is the build entrypoint called from StartBuild's
// goroutine.  It replaces the legacy tier crawl.
func (si *SearchIndex) runDumpBuild(ctx context.Context) {
	si.MarkReadyIfPopulated()

	// The catalog dump is authoritative and only grows; it is imported
	// once and never re-crawled on a timer.  Popularity freshness comes
	// from incremental dumps, and new releases from lazy per-artist
	// fetches — not from re-running this multi-GB import.
	if si.hasMeta(dumpImportDoneKey) {
		si.logger.Info("search index: dump import already complete, skipping")
		si.refreshStatusCounts()

		return
	}

	si.mu.Lock()
	si.buildStatus = IndexStatus{
		Building: true,
		Tiers: []TierStatus{
			{Name: dumpStageNames[dumpStageCounts], State: "pending"},
			{Name: dumpStageNames[dumpStageCatalog], State: "pending"},
			{Name: dumpStageNames[dumpStagePatch], State: "pending"},
			{Name: dumpStageNames[dumpStageListeners], State: "pending"},
		},
	}
	si.mu.Unlock()
	si.refreshStatusCounts()

	// Patch passes use a dedicated rate limiter so background API
	// calls never compete with interactive search/browse requests.
	var indexLB *ListenBrainzClient

	if si.lb != nil {
		indexLB = NewListenBrainzClient(
			NewRateLimiterN(indexerRate), si.lb.cache, si.logger.WithGroup("indexer"),
		)
	}

	imp, err := newDumpImporter(si, indexLB)
	if err != nil {
		si.logger.Error("search index: dump import init failed", "error", err)

		return
	}

	start := time.Now()

	if err := imp.run(ctx); err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			si.logger.Info("search index: dump import paused (will resume)",
				"elapsed", time.Since(start).Round(time.Second),
			)
		} else {
			si.logger.Error("search index: dump import failed", "error", err)
			si.setTierError(dumpStageNames[dumpStageCounts], err.Error())
		}

		return
	}

	si.mu.Lock()
	si.buildStatus.Building = false
	si.mu.Unlock()

	// Fold the local library into the freshly-imported catalog: owned
	// entities below the dump's popularity floor are inserted, and
	// dump-seeded rows that match the library are flagged in_library.
	// Deep discographies stay lazy (fetched when an artist page opens).
	si.PopulateLocalCrossReferences()

	si.refreshStatusCounts()
	si.logger.Info("search index: dump import finished",
		"elapsed", time.Since(start).Round(time.Second),
	)
}
