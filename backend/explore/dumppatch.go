//go:build indexbuild

package explore

import (
	"context"
	"strings"
)

// Stage 4 of the dump import: small, idempotent API patch passes that
// fill in what the dumps can't provide.  All calls go through the
// shared rate-limited ListenBrainz client and its HTTP cache, so
// re-running after an interruption is cheap.

const (
	// Listener-count patch budgets: only the most popular rows get
	// listener counts (a secondary ranking signal).  1000 MBIDs per
	// batched POST call → ~350 API calls total.
	listenerPatchArtists    = 50_000
	listenerPatchRGs        = 100_000
	listenerPatchRecordings = 200_000

	// popularityBatchSize is the number of MBIDs per LB popularity
	// or metadata request.  LB accepts up to 1000 per call.
	popularityBatchSize = 1000
)

// runPatchPasses fills listener counts, artist metadata, and the
// similar-artist map from the ListenBrainz API.
func (imp *dumpImporter) runPatchPasses(ctx context.Context) {
	if imp.lb == nil {
		return
	}

	imp.patchArtistMetadata(ctx)

	if ctx.Err() != nil {
		return
	}

	imp.patchSimilarArtists(ctx)

	if ctx.Err() != nil {
		return
	}

	imp.patchListenerCounts(ctx)
}

// patchArtistMetadata batch-fetches type/country/name for indexed
// artists that are missing them.  Also creates rows for kept artists
// whose name wasn't derivable from the canonical dump (multi-artist
// credits only).
func (imp *dumpImporter) patchArtistMetadata(ctx context.Context) {
	rows, err := imp.si.db.QueryContext(`
		SELECT mbid FROM explore_index
		WHERE entity_type = 1 /* artist */
		  AND (artist_type = '' OR country = '' OR title = '' OR title = mbid)
	`)
	if err != nil {
		return
	}

	var mbids []string

	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err == nil {
			mbids = append(mbids, m)
		}
	}

	_ = rows.Close()

	if len(imp.pendingArtists) > 0 {
		mbids = append(mbids, imp.pendingArtists...)
	}

	if len(mbids) == 0 {
		return
	}

	imp.logger.Info("dump import: patching artist metadata", "artists", len(mbids))

	batches := chunkStrings(mbids, popularityBatchSize)
	patched := 0

	for i, batch := range batches {
		if ctx.Err() != nil {
			return
		}

		meta, err := imp.lb.BatchArtistMetadata(ctx, batch)
		if err != nil || len(meta) == 0 {
			continue
		}

		entries := make([]SearchIndexResult, 0, len(meta))

		for mbid, m := range meta {
			if m.Name == "" {
				continue
			}

			entries = append(entries, SearchIndexResult{
				EntityType: "artist",
				MBID:       mbid,
				Title:      m.Name,
				ArtistName: m.Name,
				ArtistMBID: mbid,
				ArtistType: m.Type,
				Country:    m.Country,
			})

			// Pre-populate the MB rels cache so on-demand artist
			// image resolution skips a MusicBrainz call.
			if imp.si.artistImg != nil {
				imp.si.artistImg.PreloadArtistRels(mbid, m)
			}
		}

		imp.si.upsertBatch(entries)

		patched += len(entries)

		imp.setStageProgress(dumpStagePatch, i+1, len(batches))
	}

	imp.logger.Info("dump import: artist metadata patched", "artists", patched)
}

// patchSimilarArtists refreshes the similar-artist map for library
// artists (one API call per library artist, cached for a week).
func (imp *dumpImporter) patchSimilarArtists(ctx context.Context) {
	libraryMBIDs := imp.si.getLibraryArtistMBIDs()
	if len(libraryMBIDs) == 0 {
		return
	}

	for i := 0; i < len(libraryMBIDs); i += similarArtistsBatchSize {
		if ctx.Err() != nil {
			return
		}

		end := min(i+similarArtistsBatchSize, len(libraryMBIDs))

		grouped := imp.si.fetchSimilarArtistsBatch(ctx, imp.lb, libraryMBIDs[i:end])
		for seed, similar := range grouped {
			imp.si.storeSimilarArtists(seed, similar)

			// Flag indexed similar artists for personalized ranking.
			for _, s := range similar {
				_, _ = imp.si.db.ExecContext(
					"UPDATE explore_index SET is_similar = 1 WHERE artist_mbid = ?",
					dbMBID(s.ArtistMBID),
				)
			}
		}
	}

	imp.logger.Info("dump import: similar artists patched", "libraryArtists", len(libraryMBIDs))
}

// patchListenerCounts fills listener_count for the most popular rows
// of each entity type.  Popularity (listen count) is NOT overwritten —
// the dump-derived counts stay authoritative so the ranking scale is
// consistent across the whole index.
func (imp *dumpImporter) patchListenerCounts(ctx context.Context) {
	kinds := []struct {
		entityType string
		limit      int
		fetch      func(context.Context, []string) (map[string]PopularityData, error)
	}{
		{"artist", listenerPatchArtists, imp.lb.ArtistPopularity},
		{"release_group", listenerPatchRGs, imp.lb.ReleaseGroupPopularity},
		{"recording", listenerPatchRecordings, imp.lb.RecordingPopularity},
	}

	for _, kind := range kinds {
		if ctx.Err() != nil {
			return
		}

		mbids := imp.topMBIDs(kind.entityType, kind.limit)
		if len(mbids) == 0 {
			continue
		}

		batches := chunkStrings(mbids, popularityBatchSize)
		filled := 0

		for i, batch := range batches {
			if ctx.Err() != nil {
				return
			}

			pops, err := kind.fetch(ctx, batch)
			if err != nil {
				continue
			}

			filled += imp.si.updateListenerCounts(pops)

			imp.setStageProgress(dumpStageListeners, i+1, len(batches))
		}

		imp.logger.Info("dump import: listener counts patched",
			"entityType", kind.entityType,
			"rows", filled,
		)
	}
}

// topMBIDs returns the most popular index MBIDs for an entity type
// that don't have listener counts yet.
func (imp *dumpImporter) topMBIDs(entityType string, limit int) []string {
	rows, err := imp.si.db.QueryContext(`
		SELECT mbid FROM explore_index
		WHERE entity_type = ? AND listener_count = 0
		ORDER BY popularity DESC
		LIMIT ?
	`, dbEntityType(entityType), limit)
	if err != nil {
		return nil
	}

	defer func() { _ = rows.Close() }()

	var mbids []string

	for rows.Next() {
		var m dbMBID
		if err := rows.Scan(&m); err == nil {
			mbids = append(mbids, string(m))
		}
	}

	return mbids
}

// updateListenerCounts writes listener counts only (never popularity),
// keeping the dump-derived popularity scale consistent.  Returns the
// number of rows updated.
func (si *SearchIndex) updateListenerCounts(updates map[string]PopularityData) int {
	if len(updates) == 0 {
		return 0
	}

	tx, err := si.db.BeginTx()
	if err != nil {
		return 0
	}

	defer func() { _ = tx.Rollback() }()

	updated := 0

	for mbid, data := range updates {
		if data.ListenerCount <= 0 {
			continue
		}

		res, err := tx.Exec(
			`UPDATE explore_index
			 SET listener_count = ?
			 WHERE mbid = ? AND listener_count < ?`,
			data.ListenerCount, dbMBID(strings.ToLower(mbid)), data.ListenerCount,
		)
		if err != nil {
			continue
		}

		if n, err := res.RowsAffected(); err == nil {
			updated += int(n)
		}
	}

	_ = tx.Commit()

	return updated
}
