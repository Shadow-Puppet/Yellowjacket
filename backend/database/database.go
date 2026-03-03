// Package database provides SQLite database access.
package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"strings"

	_ "modernc.org/sqlite" // Register sqlite driver.

	"yellowjacket/backend/database/sql/sqlcgen"
	"yellowjacket/backend/profiling"
	"yellowjacket/backend/system"
)

//go:generate go tool sqlc generate

//go:embed sql/schemas/*.sql
var schemas embed.FS

// DB wraps the SQLite database connection and queries.
type DB struct {
	db      *sql.DB
	Ctx     context.Context
	Queries *sqlcgen.Queries
	logger  *slog.Logger
}

// NewDB opens the database and applies schema migrations.
func NewDB(logger *slog.Logger) (*DB, error) {
	defer profiling.TimeOp(logger, "database.NewDB")()

	dbCtx := context.Background()

	userDataDir, err := system.GetUserDataDirPath()
	if err != nil {
		return nil, fmt.Errorf("could not get user data directory: %w", err)
	}

	sqliteDBFilePath := path.Join(userDataDir, "yj.db")

	logger.Debug("opening sqlite database", "filepath", sqliteDBFilePath)

	db, err := sql.Open("sqlite", sqliteDBFilePath+"?_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("could not connect to sqlite database: %w", err)
	}

	db.SetMaxOpenConns(1) // SQLite only supports one writer at a time

	if err := applyPRAGMAs(dbCtx, db); err != nil {
		return nil, fmt.Errorf("could not apply PRAGMAs: %w", err)
	}

	// Execute SQL files from the embedded schemas directory
	logger.Debug("reading sql schema files from embedded directory")

	dirEntries, err := schemas.ReadDir("sql/schemas")
	if err != nil {
		return nil, fmt.Errorf("could not read schemas directory: %w", err)
	}

	logger.Debug("executing all sql schema files")

	for _, dirEntry := range dirEntries {
		if !dirEntry.IsDir() {
			filePath := path.Join("sql/schemas", dirEntry.Name())

			sqlContent, err := fs.ReadFile(schemas, filePath)
			if err != nil {
				return nil, fmt.Errorf("could not read file %s: %w", filePath, err)
			}

			logger.Debug(
				"executing sql schema file",
				"filepath",
				filePath,
				"sql",
				string(sqlContent),
			)

			_, err = db.ExecContext(dbCtx, string(sqlContent)) // Execute the SQL
			if err != nil {
				return nil, fmt.Errorf("error executing sql from file %s: %w", filePath, err)
			}
		}
	}

	// Run versioned schema migrations for columns that cannot be
	// added with CREATE TABLE IF NOT EXISTS on existing databases.
	if err := runMigrations(dbCtx, db, logger); err != nil {
		return nil, fmt.Errorf(
			"could not run schema migrations: %w", err,
		)
	}

	// Remove orphaned playlist_tracks left behind by past deletes
	// that ran without foreign key enforcement.
	orphanResult, err := db.ExecContext(
		dbCtx,
		"DELETE FROM playlist_tracks WHERE playlist_id NOT IN (SELECT id FROM playlists)",
	)
	if err != nil {
		logger.Warn(
			"could not clean orphaned playlist tracks",
			"err", err,
		)
	} else if n, _ := orphanResult.RowsAffected(); n > 0 {
		logger.Info(
			"Cleaned orphaned playlist tracks",
			"deleted", n,
		)
	}

	// Get generated queries
	queries := sqlcgen.New(db)

	return &DB{
		db:      db,
		Ctx:     dbCtx,
		Queries: queries,
		logger:  logger,
	}, err
}

// BeginTx starts a new database transaction.
func (d *DB) BeginTx() (*sql.Tx, error) {
	return d.db.BeginTx(d.Ctx, nil)
}

// ExecContext executes a query without returning any rows.
func (d *DB) ExecContext(query string, args ...any) (sql.Result, error) {
	return d.db.ExecContext(d.Ctx, query, args...)
}

// QueryContext executes a query that returns rows.
func (d *DB) QueryContext(query string, args ...any) (*sql.Rows, error) {
	return d.db.QueryContext(d.Ctx, query, args...)
}

// applyPRAGMAs configures SQLite connection settings. Called by both
// NewDB and NewTestDB to ensure identical behavior.
func applyPRAGMAs(ctx context.Context, db *sql.DB) error {
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA cache_size = -8000",
		"PRAGMA mmap_size = 67108864",
	}

	for _, pragma := range pragmas {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			return fmt.Errorf(
				"could not apply PRAGMA %q: %w", pragma, err,
			)
		}
	}

	return nil
}

// runMigrations applies incremental schema changes using SQLite's
// PRAGMA user_version as the version tracker.  Each migration runs
// once and bumps the version so it is never re-applied.
func runMigrations(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	var version int

	if err := db.QueryRowContext(
		ctx, "PRAGMA user_version",
	).Scan(&version); err != nil {
		return fmt.Errorf(
			"could not read user_version: %w", err,
		)
	}

	logger.Debug(
		"current schema version",
		"user_version", version,
	)

	// Migration 1: add audio-property columns to audio_files.
	if version < 1 {
		logger.Info("applying migration 1: audio file properties")

		cols := []string{
			"sample_rate int NOT NULL DEFAULT 0",
			"bit_depth int NOT NULL DEFAULT 0",
			"channels int NOT NULL DEFAULT 0",
			"bitrate int NOT NULL DEFAULT 0",
			"file_size int NOT NULL DEFAULT 0",
		}

		for _, col := range cols {
			stmt := "ALTER TABLE audio_files ADD COLUMN " + col

			if _, err := db.ExecContext(ctx, stmt); err != nil {
				// Column may already exist on a fresh DB that
				// ran the updated CREATE TABLE.  SQLite returns
				// "duplicate column name" in that case.
				if isDuplicateColumnErr(err) {
					continue
				}

				return fmt.Errorf(
					"migration 1 failed (%s): %w", col, err,
				)
			}
		}

		if _, err := db.ExecContext(
			ctx, "PRAGMA user_version = 1",
		); err != nil {
			return fmt.Errorf(
				"could not set user_version to 1: %w", err,
			)
		}
	}

	// Migration 2: add basename column and populate search index.
	if version < 2 {
		if err := migration2BasenameAndFTS(
			ctx, db, logger,
		); err != nil {
			return err
		}
	}

	// Migration 3: add UNIQUE constraint to artist_credit_artist.
	if version < 3 {
		logger.Info(
			"applying migration 3: artist_credit_artist unique constraint",
		)

		// Remove duplicates first (keep lowest ID per pair).
		if _, err := db.ExecContext(ctx, `
			DELETE FROM artist_credit_artist
			WHERE id NOT IN (
				SELECT MIN(id)
				FROM artist_credit_artist
				GROUP BY artist_id, credit_id
			)
		`); err != nil {
			return fmt.Errorf(
				"migration 3: could not deduplicate: %w", err,
			)
		}

		if _, err := db.ExecContext(ctx, `
			CREATE UNIQUE INDEX IF NOT EXISTS
				idx_artist_credit_artist_unique
			ON artist_credit_artist(artist_id, credit_id)
		`); err != nil {
			return fmt.Errorf(
				"migration 3: could not create unique index: %w",
				err,
			)
		}

		if _, err := db.ExecContext(
			ctx, "PRAGMA user_version = 3",
		); err != nil {
			return fmt.Errorf(
				"could not set user_version to 3: %w", err,
			)
		}

		logger.Info("migration 3 complete")
	}

	return nil
}

// migration2BasenameAndFTS adds the basename column to audio_files,
// backfills it from file_path, creates the basename index, and
// populates the FTS5 search_index table.
func migration2BasenameAndFTS(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info(
		"applying migration 2: basename column + FTS5 search index",
	)

	// Add basename column (may already exist on fresh DBs).
	if _, err := db.ExecContext(
		ctx,
		"ALTER TABLE audio_files ADD COLUMN basename text NOT NULL DEFAULT ''",
	); err != nil && !isDuplicateColumnErr(err) {
		return fmt.Errorf(
			"migration 2: could not add basename column: %w",
			err,
		)
	}

	// Backfill basename from file_path for existing rows.
	// SQLite doesn't have a basename function, so we use
	// REPLACE to strip directories by finding everything
	// after the last '/'.
	if _, err := db.ExecContext(ctx, `
		UPDATE audio_files
		SET basename = CASE
			WHEN INSTR(file_path, '/') > 0
			THEN SUBSTR(
				file_path,
				LENGTH(file_path)
					- LENGTH(
						REPLACE(file_path, '/', '')
					)
					+ 1
			)
			ELSE file_path
		END
		WHERE basename = ''
	`); err != nil {
		return fmt.Errorf(
			"migration 2: could not backfill basename: %w",
			err,
		)
	}

	// Create index (IF NOT EXISTS handles fresh DBs).
	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_audio_files_basename
		ON audio_files(basename)
	`); err != nil {
		return fmt.Errorf(
			"migration 2: could not create basename index: %w",
			err,
		)
	}

	// Populate FTS5 search index from existing data.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO search_index(rowid, file_path, title, artist, album)
		SELECT
			af.id,
			af.file_path,
			COALESCE(r.name, ''),
			COALESCE(ac.text, ''),
			COALESCE(rg.name, '')
		FROM audio_files af
		LEFT JOIN recordings r ON af.recording_id = r.id
		LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
		LEFT JOIN (
			SELECT recording_id,
				MIN(release_group_id) AS release_group_id
			FROM release_group_recordings
			GROUP BY recording_id
		) rgr ON r.id = rgr.recording_id
		LEFT JOIN release_groups rg
			ON rgr.release_group_id = rg.id
	`); err != nil {
		return fmt.Errorf(
			"migration 2: could not populate search index: %w",
			err,
		)
	}

	if _, err := db.ExecContext(
		ctx, "PRAGMA user_version = 2",
	); err != nil {
		return fmt.Errorf(
			"could not set user_version to 2: %w", err,
		)
	}

	logger.Info("migration 2 complete")

	return nil
}

// isDuplicateColumnErr returns true when the error is SQLite's
// "duplicate column name" error from an ALTER TABLE ADD COLUMN
// on a column that already exists.
func isDuplicateColumnErr(err error) bool {
	return err != nil &&
		strings.Contains(
			err.Error(), "duplicate column name",
		)
}
