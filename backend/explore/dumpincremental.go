package explore

import (
	"archive/tar"
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Incremental listen-count refresh.  ListenBrainz publishes a small
// (~250MB) incremental spark dump every day containing only the listens
// submitted since the previous dump, in the same parquet-in-tar format
// as the full dump.  This folds those daily deltas into the index's
// popularity numbers additively, so recordings, artists, and albums stay
// fresh without re-streaming the ~170GB full dump and without a single
// ListenBrainz API call.
//
// Album (release-group) popularity is derived locally: the incremental
// gives per-release listen counts, which are rolled up to their release
// group via the release_to_rg map captured during the full import.
//
// Correctness: each dump's deltas plus the high-water-mark advance are
// applied in one transaction, so a crash can't half-apply a dump, and
// the high-water-mark guarantees each dump is applied at most once.  The
// sum of the full dump plus every incremental is therefore the exact
// cumulative listen count — the additive model does not drift.

const (
	// defaultIncrementalBaseURL is where ListenBrainz publishes daily
	// incremental listen dumps.
	defaultIncrementalBaseURL = "https://data.metabrainz.org/pub/musicbrainz/listenbrainz/incremental/"

	// listensCatchupTsKey records when the incremental refresh last ran
	// (RFC3339), gating how often it re-checks for new dumps.
	listensCatchupTsKey = "listens_last_catchup"

	// listensCatchupInterval is the default minimum time between refresh
	// checks.  Album/track popularity is slow-moving and each dump is a
	// ~250MB download, so a weekly cadence keeps the index current while
	// bounding background network use.
	listensCatchupInterval = 7 * 24 * time.Hour

	// deltaInsertBatch bounds rows per multi-row INSERT into the temp
	// delta table during a single incremental apply.
	deltaInsertBatch = 500

	// releaseLookupBatch bounds release MBIDs per release_to_rg lookup.
	releaseLookupBatch = 500
)

var (
	incrementalDirRe  = regexp.MustCompile(`^listenbrainz-dump-\d+-\d{8}-\d+-incremental$`)
	incrementalFileRe = regexp.MustCompile(`^listenbrainz-spark-dump-.*-incremental\.tar$`)
)

// incrementalDump identifies one daily incremental dump.
type incrementalDump struct {
	series int
	url    string
}

// RefreshListenCounts folds any incremental dumps newer than the current
// high-water-mark into the index's popularity numbers.  It is a no-op
// when there is no completed baseline import, when a full build is
// running, when the last refresh was within minInterval (pass 0 to
// force), or when offline.  Runs synchronously — callers wanting the
// background behaviour should invoke it in a goroutine.
func (si *SearchIndex) RefreshListenCounts(ctx context.Context, minInterval time.Duration) {
	if !si.hasMeta(dumpImportDoneKey) {
		si.logger.Info("incremental refresh: no baseline import yet, skipping")

		return
	}

	si.mu.RLock()
	building := si.cancel != nil
	si.mu.RUnlock()

	if building {
		si.logger.Info("incremental refresh: full build running, skipping")

		return
	}

	if minInterval > 0 && si.refreshedWithin(minInterval) {
		si.logger.Info("incremental refresh: checked recently, skipping")

		return
	}

	hwm, ok := si.metaInt(listensAppliedSeriesKey)
	if !ok {
		si.logger.Warn("incremental refresh: no baseline series recorded, skipping")

		return
	}

	client := &http.Client{}

	dumps, err := discoverIncrementalDumps(ctx, client, defaultIncrementalBaseURL, hwm)
	if err != nil {
		si.logger.Warn("incremental refresh: discovery failed", "error", err)

		return
	}

	// Record the check even when there is nothing to apply, so the
	// cadence gate holds regardless of outcome.
	defer si.setMeta(listensCatchupTsKey, time.Now().UTC().Format(time.RFC3339))

	if len(dumps) == 0 {
		si.logger.Info("incremental refresh: up to date", "throughSeries", hwm)

		return
	}

	si.logger.Info("incremental refresh: applying dumps",
		"count", len(dumps), "fromSeries", hwm+1, "toSeries", dumps[len(dumps)-1].series,
	)

	applied := 0
	through := hwm

	for _, d := range dumps {
		if ctx.Err() != nil {
			break
		}

		if err := si.applyIncremental(ctx, client, d); err != nil {
			// Stop at the first failure; the high-water-mark is only
			// advanced on success, so the next run resumes from here.
			si.logger.Warn("incremental refresh: apply failed, stopping",
				"series", d.series, "error", err)

			break
		}

		applied++
		through = d.series
	}

	if applied > 0 {
		// Popularity changed, so refresh the champion tier that backs
		// generic short-prefix searches.
		si.scheduleChampionRebuild()
	}

	si.logger.Info("incremental refresh: complete", "applied", applied, "throughSeries", through)
}

// applyIncremental downloads one incremental dump, aggregates its listen
// counts, rolls release counts up to release groups, and applies the
// deltas atomically together with the high-water-mark advance.
func (si *SearchIndex) applyIncremental(
	ctx context.Context, client *http.Client, dump incrementalDump,
) error {
	start := time.Now()

	stream := newResumableReader(ctx, client, dump.url, 0)

	defer func() { _ = stream.Close() }()

	counts, err := aggregateTarListens(ctx, stream)
	if err != nil {
		return fmt.Errorf("aggregate incremental %d: %w", dump.series, err)
	}

	rec, art, rel := splitCountsByKind(counts)
	rg := si.rollupReleaseDeltas(rel)

	if err := si.commitListenDeltas(dump.series, rec, art, rg); err != nil {
		return err
	}

	si.logger.Info("incremental refresh: applied dump",
		"series", dump.series,
		"recordings", len(rec),
		"artists", len(art),
		"releaseGroups", len(rg),
		"elapsed", time.Since(start).Round(time.Millisecond),
	)

	return nil
}

// splitCountsByKind separates an aggregated counts map into per-kind
// maps of canonical MBID string → delta.
func splitCountsByKind(counts map[mbidKey]uint32) (rec, art, rel map[string]uint32) {
	rec = make(map[string]uint32)
	art = make(map[string]uint32)
	rel = make(map[string]uint32)

	for k, v := range counts {
		mbid := formatUUID(k[1:])

		switch k[0] {
		case countKindRecording:
			rec[mbid] += v
		case countKindArtist:
			art[mbid] += v
		case countKindRelease:
			rel[mbid] += v
		}
	}

	return rec, art, rel
}

// rollupReleaseDeltas maps per-release listen deltas to their release
// group via the release_to_rg table and sums per group.  Releases not in
// the table (below the index floor, or unknown) contribute nothing.
func (si *SearchIndex) rollupReleaseDeltas(rel map[string]uint32) map[string]uint32 {
	rg := make(map[string]uint32)
	if len(rel) == 0 {
		return rg
	}

	mbids := make([]string, 0, len(rel))
	for m := range rel {
		mbids = append(mbids, m)
	}

	for i := 0; i < len(mbids); i += releaseLookupBatch {
		end := min(i+releaseLookupBatch, len(mbids))
		batch := mbids[i:end]

		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")

		args := make([]any, len(batch))
		for j, m := range batch {
			args[j] = m
		}

		rows, err := si.db.QueryContext(
			"SELECT release_mbid, rg_mbid FROM release_to_rg WHERE release_mbid IN ("+placeholders+")",
			args...,
		)
		if err != nil {
			si.logger.Warn("incremental refresh: release_to_rg lookup failed", "error", err)

			continue
		}

		for rows.Next() {
			var relMBID, rgMBID string
			if err := rows.Scan(&relMBID, &rgMBID); err == nil {
				rg[rgMBID] += rel[relMBID]
			}
		}

		_ = rows.Close()
	}

	return rg
}

// commitListenDeltas applies recording, artist, and release-group deltas
// to explore_index and advances the high-water-mark to series, all in a
// single transaction so the apply is crash-atomic and exactly-once.
func (si *SearchIndex) commitListenDeltas(
	series int, rec, art, rg map[string]uint32,
) error {
	tx, err := si.db.BeginTx()
	if err != nil {
		return fmt.Errorf("incremental tx: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(
		"CREATE TEMP TABLE IF NOT EXISTS incr_delta (mbid TEXT, kind TEXT, delta INTEGER)",
	); err != nil {
		return fmt.Errorf("incremental temp table: %w", err)
	}

	if _, err := tx.Exec("DELETE FROM incr_delta"); err != nil {
		return fmt.Errorf("incremental temp reset: %w", err)
	}

	for _, kd := range []struct {
		kind   string
		deltas map[string]uint32
	}{
		{"recording", rec},
		{"artist", art},
		{"release_group", rg},
	} {
		if err := insertDeltas(tx, kd.kind, kd.deltas); err != nil {
			return err
		}
	}

	// Additive apply: bump popularity for every index row that has a
	// matching delta.  Rows with no delta are untouched; deltas with no
	// matching row (entity not indexed) are ignored.
	if _, err := tx.Exec(`
		UPDATE explore_index
		SET popularity = popularity + d.delta
		FROM incr_delta d
		WHERE d.mbid = explore_index.mbid
		  AND d.kind = explore_index.entity_type
	`); err != nil {
		return fmt.Errorf("incremental apply: %w", err)
	}

	if _, err := tx.Exec("DELETE FROM incr_delta"); err != nil {
		return fmt.Errorf("incremental temp cleanup: %w", err)
	}

	if _, err := tx.Exec(
		"INSERT OR REPLACE INTO explore_index_meta (key, value) VALUES (?, ?)",
		listensAppliedSeriesKey, strconv.Itoa(series),
	); err != nil {
		return fmt.Errorf("incremental advance high-water-mark: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("incremental commit: %w", err)
	}

	return nil
}

// insertDeltas bulk-inserts a kind's deltas into the temp table.
func insertDeltas(tx *sql.Tx, kind string, deltas map[string]uint32) error {
	if len(deltas) == 0 {
		return nil
	}

	rowArgs := make([]any, 0, deltaInsertBatch*3)
	pending := 0

	flush := func() error {
		if pending == 0 {
			return nil
		}

		values := strings.TrimSuffix(strings.Repeat("(?,?,?),", pending), ",")
		query := "INSERT INTO incr_delta (mbid, kind, delta) VALUES " + values

		if _, err := tx.Exec(query, rowArgs...); err != nil {
			return fmt.Errorf("incremental insert deltas: %w", err)
		}

		rowArgs = rowArgs[:0]
		pending = 0

		return nil
	}

	for mbid, d := range deltas {
		rowArgs = append(rowArgs, mbid, kind, int64(d))
		pending++

		if pending >= deltaInsertBatch {
			if err := flush(); err != nil {
				return err
			}
		}
	}

	return flush()
}

// aggregateTarListens streams a tar of parquet listen members and sums
// the per-entity listen counts.  Unlike the full-dump path, the whole
// (small) incremental is aggregated in RAM with no checkpointing.
func aggregateTarListens(ctx context.Context, r io.Reader) (map[mbidKey]uint32, error) {
	buffered := bufio.NewReaderSize(r, 1<<20)
	tr := tar.NewReader(buffered)
	counts := make(map[mbidKey]uint32, 1<<18)

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("incremental tar: %w", err)
		}

		if hdr.Typeflag != tar.TypeReg || !strings.HasSuffix(hdr.Name, ".parquet") {
			continue
		}

		if hdr.Size > maxParquetMemberSize {
			return nil, fmt.Errorf(
				"%w: parquet member %s is %d bytes", ErrDumpFormat, hdr.Name, hdr.Size,
			)
		}

		buf := make([]byte, hdr.Size)
		if _, err := io.ReadFull(tr, buf); err != nil {
			return nil, fmt.Errorf("incremental member read: %w", err)
		}

		deltas, err := parseListenParquet(buf)
		if err != nil {
			return nil, fmt.Errorf("incremental parse: %w", err)
		}

		for k, v := range deltas {
			counts[k] += v
		}
	}

	return counts, nil
}

// discoverIncrementalDumps lists the incremental directory and returns
// the spark-dump URLs for every dump with a series greater than
// sinceSeries, sorted ascending so they apply in chronological order.
func discoverIncrementalDumps(
	ctx context.Context, client *http.Client, baseURL string, sinceSeries int,
) ([]incrementalDump, error) {
	hrefs, err := listHrefs(ctx, client, baseURL)
	if err != nil {
		return nil, err
	}

	var dumps []incrementalDump

	for _, h := range hrefs {
		dir := trimTrailingSlash(h)
		if !incrementalDirRe.MatchString(dir) {
			continue
		}

		series, ok := parseDumpSeries(dir)
		if !ok || series <= sinceSeries {
			continue
		}

		dirURL := baseURL + dir + "/"

		files, err := listHrefs(ctx, client, dirURL)
		if err != nil {
			continue
		}

		for _, f := range files {
			if incrementalFileRe.MatchString(f) {
				dumps = append(dumps, incrementalDump{series: series, url: dirURL + f})

				break
			}
		}
	}

	sort.Slice(dumps, func(i, j int) bool { return dumps[i].series < dumps[j].series })

	return dumps, nil
}

// metaInt reads an integer-valued explore_index_meta key.
func (si *SearchIndex) metaInt(key string) (int, bool) {
	rows, err := si.db.QueryContext("SELECT value FROM explore_index_meta WHERE key = ?", key)
	if err != nil {
		return 0, false
	}

	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return 0, false
	}

	var v string
	if err := rows.Scan(&v); err != nil {
		return 0, false
	}

	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}

	return n, true
}

// refreshedWithin reports whether the incremental refresh last ran less
// than d ago.
func (si *SearchIndex) refreshedWithin(d time.Duration) bool {
	rows, err := si.db.QueryContext(
		"SELECT value FROM explore_index_meta WHERE key = ?", listensCatchupTsKey,
	)
	if err != nil {
		return false
	}

	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return false
	}

	var v string
	if err := rows.Scan(&v); err != nil {
		return false
	}

	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return false
	}

	return time.Since(t) < d
}
