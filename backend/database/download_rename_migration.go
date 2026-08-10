package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// migrateDownloadRename performs the download subsystem's table rename
// for existing databases that still carry the old table names: the
// durable "I asked for this" record moved from download_wants to
// download_requests, and the one-shot search-and-grab attempt moved
// from download_requests to download_downloads (see CLAUDE.md and
// .planning/NOTES.md for the full Want->Request / Request->Download
// rename).
//
// This cannot be a plain sql/migrations file the way an ADD COLUMN
// migration is. That pattern's tolerance for "duplicate column name"
// works because a fresh database's sql/schemas pass already produces
// the identical target shape under the identical table name, so
// replaying the ALTER TABLE against it is a safe no-op. Here the name
// "download_requests" is reused for a different table before and after
// the rename, so a fresh database's schema pass creates a real, empty,
// correctly-shaped download_downloads AND a real, empty,
// correctly-shaped (new) download_requests before this ever runs.
// Blindly replaying "ALTER TABLE download_requests RENAME TO
// download_downloads" against that fresh database would rename the new,
// empty Request table into Download's place, destroying the fresh
// install rather than no-opping. Gating on whether the OLD
// download_wants table still exists — a name nothing creates or
// references once this has run — is what tells an old database and a
// fresh (or already migrated) one apart without executing anything
// destructive on the fresh path.
func migrateDownloadRename(ctx context.Context, db *sql.DB) error {
	var name string

	err := db.QueryRowContext(
		ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'download_wants'`,
	).Scan(&name)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		// Nothing to migrate: either a fresh install (sql/schemas
		// already produced the target shape) or a database this has
		// already run against.
	case err != nil:
		return fmt.Errorf("check for download_wants table: %w", err)
	default:
		if err := runDownloadRename(ctx, db); err != nil {
			return err
		}
	}

	return ensureDownloadIndexes(ctx, db)
}

// runDownloadRename performs the actual rename dance against a
// database confirmed to still have the old download_wants table.
func runDownloadRename(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		// The schema pass already created an empty, correctly-shaped
		// download_downloads placeholder under this name (it never
		// existed under the old naming), which would otherwise collide
		// with the rename below.
		`DROP TABLE IF EXISTS download_downloads`,

		// 1. Free the "download_requests" name: the old one-shot
		// attempt table becomes download_downloads.
		`ALTER TABLE download_requests RENAME TO download_downloads`,
		`ALTER TABLE download_downloads RENAME COLUMN want_id TO request_id`,

		// 2. Claim the now-free "download_requests" name for the
		// durable-intent table.
		`ALTER TABLE download_wants RENAME TO download_requests`,

		// 3. The transfer table's FK now points at download_downloads.
		`ALTER TABLE download_items RENAME COLUMN request_id TO download_id`,

		// Named indexes survive a table/column rename attached to their
		// old name, so drop them here; ensureDownloadIndexes recreates
		// them under the names sql/schemas' comments describe.
		`DROP INDEX IF EXISTS idx_download_requests_created`,
		`DROP INDEX IF EXISTS idx_download_requests_state`,
		`DROP INDEX IF EXISTS idx_download_wants_due`,
		`DROP INDEX IF EXISTS idx_download_wants_entity`,
		`DROP INDEX IF EXISTS idx_download_wants_parent`,
		`DROP INDEX IF EXISTS idx_download_items_request`,
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin download rename migration: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("download rename migration %q: %w", stmt, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit download rename migration: %w", err)
	}

	return nil
}

// ensureDownloadIndexes creates the indexes sql/schemas deliberately
// omits inline for the renamed table/columns (see
// migrateDownloadRename), under their final names. Safe to call
// unconditionally: IF NOT EXISTS makes it a no-op once created, and by
// the time this runs every column/table involved is guaranteed to be
// in its final shape on both a fresh and a migrated database.
func ensureDownloadIndexes(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`CREATE INDEX IF NOT EXISTS idx_download_downloads_created
		    ON download_downloads(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_download_downloads_state
		    ON download_downloads(state)`,
		`CREATE INDEX IF NOT EXISTS idx_download_requests_due
		    ON download_requests(next_try_at) WHERE state = 'wanted'`,
		`CREATE INDEX IF NOT EXISTS idx_download_requests_entity
		    ON download_requests(entity, state)`,
		`CREATE INDEX IF NOT EXISTS idx_download_requests_parent
		    ON download_requests(parent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_download_items_download
		    ON download_items(download_id)`,
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("ensure download index: %w", err)
		}
	}

	return nil
}
