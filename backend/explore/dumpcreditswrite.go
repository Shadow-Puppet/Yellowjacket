//go:build indexbuild

package explore

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

// writeCredits persists the scanned decompositions.
//
// Only credits some catalog entity actually points at are written: the
// dump has millions of multi-artist credits and the catalog keeps ~1.8M
// entities, so storing every credit would be most of a table nothing
// can reach.
//
// The two tables are written in one transaction, because a ref pointing
// at parts that are not there renders as a credit with no artists --
// worse than the single-artist fallback it replaced.
func (imp *dumpImporter) writeCredits(ctx context.Context, scan *creditScan) error {
	tx, err := imp.si.db.BeginTx()
	if err != nil {
		return fmt.Errorf("credit import: begin: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	// A rebuild replaces the previous pass wholesale.  These are Cache
	// tables derived entirely from the dump, so there is nothing to
	// merge and a stale row is a wrong credit.
	for _, table := range []string{"artist_credit_part", "artist_credit_ref"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return fmt.Errorf("credit import: clear %s: %w", table, err)
		}
	}

	written, err := imp.writeCreditParts(ctx, tx, scan)
	if err != nil {
		return err
	}

	refs, err := imp.writeCreditRefs(ctx, tx, scan, written)
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("credit import: commit: %w", err)
	}

	imp.logger.Info("credit import: complete",
		"credits", len(written),
		"refs", refs,
	)

	return nil
}

// writeCreditParts inserts the parts of every used credit and returns
// the set of credits that were actually stored.
//
// A credit is stored whole or not at all.  If any of its artists has no
// MBID -- which should not happen, the dump being self-consistent, but
// would leave a part that cannot be navigated to -- the credit is
// dropped and the entity falls back to explore_index's single artist,
// which is a worse answer rather than a broken one.
func (imp *dumpImporter) writeCreditParts(
	ctx context.Context, tx *sql.Tx, scan *creditScan,
) (map[int32]struct{}, error) {
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO artist_credit_part
			(credit_id, position, artist_mbid, credited_name, join_phrase)
		 VALUES (?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return nil, fmt.Errorf("credit import: prepare part insert: %w", err)
	}

	defer func() { _ = stmt.Close() }()

	written := make(map[int32]struct{}, len(scan.used))

	for credit := range scan.used {
		parts := scan.parts[credit]
		if len(parts) < 2 {
			// artist_credit said more than one artist and
			// artist_credit_name did not deliver them.  Nothing to
			// decompose, so leave the entity to its single artist.
			continue
		}

		// Position order is the credit's meaning, and the dump is not
		// obliged to emit it sorted.
		sort.Slice(parts, func(i, j int) bool {
			return parts[i].position < parts[j].position
		})

		resolved := make([][]any, 0, len(parts))
		ok := true

		for _, part := range parts {
			gid, found := scan.artistGIDs[part.artistID]
			if !found {
				scan.skippedUnknownArtist++
				ok = false

				break
			}

			resolved = append(resolved, []any{
				credit, part.position, gid[:], part.name, part.join,
			})
		}

		if !ok {
			continue
		}

		for _, args := range resolved {
			if _, err := stmt.ExecContext(ctx, args...); err != nil {
				return nil, fmt.Errorf("credit import: insert part: %w", err)
			}
		}

		written[credit] = struct{}{}
	}

	return written, nil
}

// writeCreditRefs points each kept entity at its credit, skipping any
// whose credit was not stored so a ref never dangles.
func (imp *dumpImporter) writeCreditRefs(
	ctx context.Context, tx *sql.Tx, scan *creditScan, written map[int32]struct{},
) (int, error) {
	stmt, err := tx.PrepareContext(ctx,
		"INSERT OR REPLACE INTO artist_credit_ref (mbid, credit_id) VALUES (?, ?)",
	)
	if err != nil {
		return 0, fmt.Errorf("credit import: prepare ref insert: %w", err)
	}

	defer func() { _ = stmt.Close() }()

	count := 0

	for mbid, credit := range scan.refs {
		if _, stored := written[credit]; !stored {
			continue
		}

		id := mbid

		if _, err := stmt.ExecContext(ctx, id[:], credit); err != nil {
			return 0, fmt.Errorf("credit import: insert ref: %w", err)
		}

		count++
	}

	return count, nil
}
