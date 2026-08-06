package explore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	_ "modernc.org/sqlite" // SQLite driver for reading the artifact file.

	"yellowjacket/backend/jobs"
)

// Import of the prebuilt "core" index artifact.
//
// The catalog half of the index is identical for every user, so deriving
// it on each machine means every install streams ~89GB from
// data.metabrainz.org — a server that caps a client at roughly 2MB/s, so
// better than half a day of downloading to reach a result everyone else
// already has.  Instead CI runs that import once (cmd/indexbuild),
// exports a subset (cmd/indexexport), and clients merge the resulting
// artifact in seconds.
//
// The artifact is an ordinary SQLite database holding two tables:
// explore_index with the global catalog columns only, and artifact_meta
// describing what it is.  It deliberately carries no FTS table and no
// triggers — rows land in the client's own explore_index, whose AFTER
// INSERT trigger populates the search index as a side effect.
//
// Merging goes through the same ON CONFLICT rules as every other index
// write (upsertIndexConflictSQL), so an artifact can be applied over an
// existing index without clobbering better data: non-empty values win
// over empty, higher listen counts win over lower, and the personal
// columns the artifact does not carry are left untouched.

const (
	// coreArtifactVersionKey records which artifact version was merged,
	// so a client can tell whether it already has one and skip re-import.
	coreArtifactVersionKey = "core_artifact_version"

	// supportedArtifactVersion is the artifact schema this build knows how
	// to read.  The exporter stamps it into artifact_meta; a mismatch is
	// refused rather than guessed at, because an artifact written against
	// a different explore_index schema would merge wrong columns.
	supportedArtifactVersion = "1"
)

// artifactMergeBatch bounds how many rows are merged per transaction.
// Large enough that per-transaction overhead disappears, small enough
// that a cancelled import doesn't roll back minutes of work.  A var so
// tests can shrink it and still cross several batch boundaries.
var artifactMergeBatch = 50_000

var (
	// ErrArtifactUnusable means the file is not a core index artifact this
	// build can merge.  Callers treat it as "fall back to a normal build"
	// rather than as a fatal error.
	ErrArtifactUnusable = errors.New("core index artifact unusable")

	// ErrArtifactVersion is a version mismatch between the artifact and
	// this build.
	ErrArtifactVersion = errors.New("core index artifact version mismatch")
)

// artifactInfo is what the artifact declares about itself.
type artifactInfo struct {
	version string

	// builtAt is when the source index finished importing, not when the
	// artifact was exported.
	builtAt string

	// listensSeries is the incremental listens dump the artifact's
	// popularity numbers are baselined on.  Stamped into the client's
	// index so RefreshListenCounts resumes from the right point instead
	// of reapplying deltas already folded in.
	listensSeries string

	rows int
}

// artifactCatalogColumns are the columns an artifact carries.  It is the
// global catalog only: the personal columns (in_library, is_similar,
// local_*) describe one person's library and are recomputed locally by
// PopulateLocalCrossReferences.
//
// Kept in sync with cmd/indexexport's catalogColumns by
// TestArtifactColumnsMatchExporter.
const artifactCatalogColumns = `entity_type, mbid, title, artist_name, artist_mbid,
	aliases, popularity, listener_count, duration, caa_release_mbid,
	release_name, primary_type, secondary_types, release_date,
	artist_type, country, disambiguation, sort_name, discog_fetched`

// inspectArtifact opens the artifact read-only and reports what it
// declares, without touching the live index.  Validation happens here so
// a bad download is rejected before anything is attached.
func inspectArtifact(path string) (artifactInfo, error) {
	var info artifactInfo

	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return info, fmt.Errorf("%w: open: %w", ErrArtifactUnusable, err)
	}

	defer func() { _ = db.Close() }()

	meta := map[string]string{}

	rows, err := db.Query("SELECT key, value FROM artifact_meta")
	if err != nil {
		return info, fmt.Errorf("%w: read artifact_meta: %w", ErrArtifactUnusable, err)
	}

	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return info, fmt.Errorf("%w: scan artifact_meta: %w", ErrArtifactUnusable, err)
		}

		meta[k] = v
	}

	if err := rows.Err(); err != nil {
		return info, fmt.Errorf("%w: read artifact_meta: %w", ErrArtifactUnusable, err)
	}

	info.version = meta["artifact_version"]
	info.builtAt = meta["built_at"]
	info.listensSeries = meta["listens_applied_series"]

	if info.version != supportedArtifactVersion {
		return info, fmt.Errorf("%w: artifact is version %q, this build reads %q",
			ErrArtifactVersion, info.version, supportedArtifactVersion)
	}

	// A structurally valid but empty artifact would merge cleanly and
	// leave Explore just as empty as before, while stamping the index as
	// imported.  Refuse it.
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM explore_index",
	).Scan(&info.rows); err != nil {
		return info, fmt.Errorf("%w: count rows: %w", ErrArtifactUnusable, err)
	}

	if info.rows == 0 {
		return info, fmt.Errorf("%w: artifact contains no rows", ErrArtifactUnusable)
	}

	return info, nil
}

// importCoreArtifact merges a validated artifact at path into the live
// index.  It is idempotent — the merge is an upsert keyed by MBID, so a
// re-run over an already-imported artifact is a no-op in effect.
//
// The caller keeps ownership of the file; nothing here deletes it.
func (si *SearchIndex) importCoreArtifact(ctx context.Context, path string) error {
	info, err := inspectArtifact(path)
	if err != nil {
		return err
	}

	si.logger.Info("core artifact: merging",
		"rows", info.rows,
		"builtAt", info.builtAt,
		"listensSeries", info.listensSeries,
	)
	si.logIndexJob(jobs.LevelInfo, fmt.Sprintf(
		"Merging prebuilt catalog (%s rows, built %s)",
		formatCount(info.rows), info.builtAt,
	))

	// ATTACH cannot run inside a transaction, and it binds to a single
	// connection — which is why every statement below goes through the
	// writer (SetMaxOpenConns(1)).  Reads must not use db.QueryContext:
	// that routes to the separate read pool, where "core" does not exist.
	if _, err := si.db.ExecContext(`ATTACH DATABASE ? AS core`, path); err != nil {
		return fmt.Errorf("%w: attach: %w", ErrArtifactUnusable, err)
	}

	defer func() {
		if _, err := si.db.ExecContext(`DETACH DATABASE core`); err != nil {
			si.logger.Warn("core artifact: detach failed", "error", err)
		}
	}()

	// Per-row FTS maintenance across a million inserts costs far more
	// than the inserts themselves (~31 rows/s against ~4,700), so the
	// search index is rebuilt once at the end instead.
	ftsSuspended := true

	if err := si.db.SuspendExploreIndexFTS(); err != nil {
		si.logger.Warn("core artifact: could not suspend FTS sync", "error", err)

		ftsSuspended = false
	}

	merged, mergeErr := si.mergeArtifactRows(ctx, info.rows)

	if ftsSuspended {
		start := time.Now()

		if err := si.db.ResumeExploreIndexFTS(); err != nil {
			// Leaving search unindexed is worse than a slow import: this
			// needs a rebuild to recover, so it is loud.
			si.logger.Error("core artifact: FTS rebuild failed — search index is stale",
				"error", err,
			)
		} else {
			si.logger.Info("core artifact: FTS index rebuilt",
				"elapsed", time.Since(start).Round(time.Millisecond),
			)
		}
	}

	if mergeErr != nil {
		return mergeErr
	}

	si.stampArtifactMeta(info)
	si.analyzeIndex()

	si.logger.Info("core artifact: merge complete", "rows", merged)
	si.logIndexJob(jobs.LevelInfo, fmt.Sprintf(
		"Prebuilt catalog merged (%s rows)", formatCount(merged),
	))

	si.MarkReadyIfPopulated()
	si.refreshStatusCounts()

	return nil
}

// analyzeIndex refreshes the query planner's table statistics.
//
// It runs here rather than at schema creation because an empty database
// has nothing to measure: the numbers that matter only exist once the
// catalog has been merged.  Without them the planner mis-estimates the
// partial expression indexes on explore_index and falls back to scanning
// a million rows for queries that should seek.
func (si *SearchIndex) analyzeIndex() {
	start := time.Now()

	if _, err := si.db.ExecContext("ANALYZE"); err != nil {
		// Only a performance loss, so it must not fail the import.
		si.logger.Warn("core artifact: ANALYZE failed", "error", err)

		return
	}

	si.logger.Info("core artifact: query planner statistics refreshed",
		"elapsed", time.Since(start).Round(time.Millisecond),
	)
}

// mergeArtifactRows copies the attached artifact into explore_index in
// bounded batches, walking the artifact's MBID primary key so each batch
// is an index range scan and a cancelled import leaves committed work
// behind rather than rolling it all back.
func (si *SearchIndex) mergeArtifactRows(ctx context.Context, total int) (int, error) {
	insertSQL := `
		INSERT INTO explore_index (` + artifactCatalogColumns + `)
		SELECT ` + artifactCatalogColumns + `
		FROM core.explore_index
		WHERE mbid > ?` + upsertIndexConflictSQL

	// The final batch has no upper bound, so the range predicate is
	// appended only while one exists.
	insertRangeSQL := `
		INSERT INTO explore_index (` + artifactCatalogColumns + `)
		SELECT ` + artifactCatalogColumns + `
		FROM core.explore_index
		WHERE mbid > ? AND mbid <= ?` + upsertIndexConflictSQL

	var (
		cursor string
		merged int
	)

	for {
		if err := ctx.Err(); err != nil {
			return merged, err
		}

		upper, hasUpper, err := si.artifactBatchBound(cursor)
		if err != nil {
			return merged, err
		}

		var res sql.Result

		if hasUpper {
			res, err = si.db.ExecContext(insertRangeSQL, cursor, upper)
		} else {
			res, err = si.db.ExecContext(insertSQL, cursor)
		}

		if err != nil {
			return merged, fmt.Errorf("%w: merge batch: %w", ErrArtifactUnusable, err)
		}

		n, err := res.RowsAffected()
		if err != nil {
			return merged, fmt.Errorf("%w: merge batch rows: %w", ErrArtifactUnusable, err)
		}

		merged += int(n)

		si.setTierDetail(
			artifactStageNames[artifactStageMerge], "running", merged, total,
			fmt.Sprintf("%s of %s rows", formatCount(merged), formatCount(total)),
		)

		if !hasUpper {
			return merged, nil
		}

		cursor = upper
	}
}

// artifactBatchBound returns the MBID that ends the next batch, and
// whether one exists — no bound means the remainder is the last batch.
func (si *SearchIndex) artifactBatchBound(cursor string) (string, bool, error) {
	var bound string

	err := si.db.QueryRowWriter(
		`SELECT mbid FROM core.explore_index
		 WHERE mbid > ? ORDER BY mbid LIMIT 1 OFFSET ?`,
		cursor, artifactMergeBatch-1,
	).Scan(&bound)

	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}

	if err != nil {
		return "", false, fmt.Errorf("%w: batch bound: %w", ErrArtifactUnusable, err)
	}

	return bound, true, nil
}

// stampArtifactMeta records what the merge established: the catalog half
// is populated, and popularity is baselined on the artifact's listens
// series so the incremental refresh resumes from there.
func (si *SearchIndex) stampArtifactMeta(info artifactInfo) {
	si.setMeta(coreArtifactVersionKey, info.version)

	// The catalog is present, so nothing should trigger a full dump
	// import on top of the artifact it was meant to replace.
	si.setMeta(dumpImportDoneKey, time.Now().UTC().Format(time.RFC3339))

	// Without a baseline series RefreshListenCounts refuses to run at
	// all, so an artifact exported before that key existed leaves the
	// index permanently frozen at its shipped popularity.  Better to say
	// so than to fail silently.
	if info.listensSeries == "" {
		si.logger.Warn(
			"core artifact: no listens series recorded — " +
				"popularity refresh will not run until the next full import",
		)

		return
	}

	if _, err := strconv.Atoi(info.listensSeries); err != nil {
		si.logger.Warn("core artifact: unparseable listens series",
			"value", info.listensSeries,
		)

		return
	}

	si.setMeta(listensAppliedSeriesKey, info.listensSeries)
}

// removeArtifactFile deletes a merged artifact.  Best-effort: a leftover
// file costs disk, not correctness.
func (si *SearchIndex) removeArtifactFile(path string) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		si.logger.Warn("core artifact: cleanup failed", "path", path, "error", err)
	}
}
