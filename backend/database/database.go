// Package database provides SQLite database access.
package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
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
	if err := runMigrations(dbCtx, db, logger, sqliteDBFilePath); err != nil {
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
	dbPath string,
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

	// Migration 4: create track_metadata VIEW.
	if version < 4 {
		if err := migration4TrackMetadataView(
			ctx, db, logger,
		); err != nil {
			return err
		}
	}

	// Migration 5: rebuild release_groups with composite unique
	// constraint on (name, album_artist_credit_id) instead of
	// name alone, so albums with the same name by different
	// artists are stored as separate rows.
	if version < 5 {
		if err := migration5ReleaseGroupCompositeUnique(
			ctx, db, logger,
		); err != nil {
			return err
		}
	}

	// Migration 6: multi-library support.
	if version < 6 {
		if err := migration6MultiLibrary(
			ctx, db, logger, dbPath,
		); err != nil {
			return err
		}
	}

	// Migration 7: add phantom_file_path to playlist_tracks
	// for automatic phantom resolution after library re-scans.
	if version < 7 {
		if err := migration7PhantomFilePath(
			ctx, db, logger,
		); err != nil {
			return err
		}
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

// migration4TrackMetadataView creates the track_metadata VIEW that
// consolidates the 5-table JOIN used by FTS5 search queries.
// Fresh databases get the VIEW from the embedded schema file;
// this migration covers databases created before the VIEW existed.
func migration4TrackMetadataView(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info(
		"applying migration 4: track_metadata VIEW",
	)

	if _, err := db.ExecContext(ctx, `
		CREATE VIEW IF NOT EXISTS track_metadata AS
		SELECT
			af.id,
			af.file_path,
			af.length_milliseconds,
			COALESCE(r.name, '') AS title,
			COALESCE(ac.text, '') AS artist_name,
			r.track_number,
			r.disc_number,
			COALESCE(rg.name, '') AS album,
			CAST(COALESCE(
				(SELECT GROUP_CONCAT(g.name, '||')
				 FROM recording_genres rg_sub
				 JOIN genres g ON rg_sub.genre_id = g.id
				 WHERE rg_sub.recording_id = r.id),
				''
			) AS TEXT) AS genre,
			COALESCE(r.year, 0) AS year,
			COALESCE(r.composer, '') AS composer,
			COALESCE(ft.extension, '') AS file_type,
			af.sample_rate,
			af.bit_depth,
			af.channels,
			af.bitrate,
			af.file_size
		FROM audio_files af
		LEFT JOIN recordings r ON af.recording_id = r.id
		LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
		LEFT JOIN (
			SELECT recording_id,
				MIN(release_group_id) AS release_group_id
			FROM release_group_recordings
			GROUP BY recording_id
		) rgr ON r.id = rgr.recording_id
		LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
		LEFT JOIN file_types ft ON af.file_type_id = ft.id
	`); err != nil {
		return fmt.Errorf(
			"migration 4: could not create track_metadata VIEW: %w",
			err,
		)
	}

	if _, err := db.ExecContext(
		ctx, "PRAGMA user_version = 4",
	); err != nil {
		return fmt.Errorf(
			"could not set user_version to 4: %w", err,
		)
	}

	logger.Info("migration 4 complete")

	return nil
}

// migration5ReleaseGroupCompositeUnique rebuilds the release_groups
// table with UNIQUE(name, album_artist_credit_id) instead of
// UNIQUE(name).  SQLite cannot ALTER a UNIQUE constraint, so we
// must rebuild the table.
//
// SAFETY: Hand-crafted SQL for schema migration.
func migration5ReleaseGroupCompositeUnique(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info(
		"applying migration 5: release_groups composite unique constraint",
	)

	// Temporarily disable FK checks for table rebuild.
	if _, err := db.ExecContext(
		ctx, "PRAGMA foreign_keys = OFF",
	); err != nil {
		return fmt.Errorf(
			"migration 5: could not disable foreign keys: %w",
			err,
		)
	}

	// Drop the track_metadata VIEW that references release_groups
	// so the table rebuild can proceed without SQLite complaining
	// about a dangling VIEW reference.
	if _, err := db.ExecContext(
		ctx, "DROP VIEW IF EXISTS track_metadata",
	); err != nil {
		return fmt.Errorf(
			"migration 5: could not drop track_metadata VIEW: %w",
			err,
		)
	}

	// Create new table with composite unique constraint.
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE release_groups_new (
			id                     INTEGER PRIMARY KEY,
			name                   TEXT NOT NULL,
			cover_art_id           INTEGER,
			album_artist_credit_id INTEGER,
			year                   INTEGER,
			total_tracks           INTEGER,
			total_discs            INTEGER,
			FOREIGN KEY(cover_art_id) REFERENCES cover_art(id),
			FOREIGN KEY(album_artist_credit_id) REFERENCES artist_credit(id),
			UNIQUE(name, album_artist_credit_id)
		)
	`); err != nil {
		return fmt.Errorf(
			"migration 5: could not create release_groups_new: %w",
			err,
		)
	}

	// Copy all data.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO release_groups_new
		SELECT * FROM release_groups
	`); err != nil {
		return fmt.Errorf(
			"migration 5: could not copy data: %w", err,
		)
	}

	// Drop old table.
	if _, err := db.ExecContext(
		ctx, "DROP TABLE release_groups",
	); err != nil {
		return fmt.Errorf(
			"migration 5: could not drop old table: %w", err,
		)
	}

	// Rename new table.
	if _, err := db.ExecContext(ctx, `
		ALTER TABLE release_groups_new
		RENAME TO release_groups
	`); err != nil {
		return fmt.Errorf(
			"migration 5: could not rename table: %w", err,
		)
	}

	// Recreate indexes.
	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_release_groups_cover_art_id
			ON release_groups(cover_art_id)
	`); err != nil {
		return fmt.Errorf(
			"migration 5: could not create cover_art_id index: %w",
			err,
		)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_release_groups_album_artist_credit_id
			ON release_groups(album_artist_credit_id)
	`); err != nil {
		return fmt.Errorf(
			"migration 5: could not create album_artist_credit_id index: %w",
			err,
		)
	}

	// Recreate the track_metadata VIEW that was dropped above.
	// The definition must match the embedded schema file
	// (sql/schemas/track_metadata_view.sql) exactly.
	if _, err := db.ExecContext(ctx, `
		CREATE VIEW IF NOT EXISTS track_metadata AS
		SELECT
			af.id,
			af.file_path,
			af.length_milliseconds,
			COALESCE(r.name, '') AS title,
			COALESCE(ac.text, '') AS artist_name,
			r.track_number,
			r.disc_number,
			COALESCE(rg.name, '') AS album,
			CAST(COALESCE(
				(SELECT GROUP_CONCAT(g.name, '||')
				 FROM recording_genres rg_sub
				 JOIN genres g ON rg_sub.genre_id = g.id
				 WHERE rg_sub.recording_id = r.id),
				''
			) AS TEXT) AS genre,
			COALESCE(r.year, 0) AS year,
			COALESCE(r.composer, '') AS composer,
			COALESCE(ft.extension, '') AS file_type,
			af.sample_rate,
			af.bit_depth,
			af.channels,
			af.bitrate,
			af.file_size
		FROM audio_files af
		LEFT JOIN recordings r ON af.recording_id = r.id
		LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
		LEFT JOIN (
			SELECT recording_id,
				MIN(release_group_id) AS release_group_id
			FROM release_group_recordings
			GROUP BY recording_id
		) rgr ON r.id = rgr.recording_id
		LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
		LEFT JOIN file_types ft ON af.file_type_id = ft.id
	`); err != nil {
		return fmt.Errorf(
			"migration 5: could not recreate track_metadata VIEW: %w",
			err,
		)
	}

	// Re-enable FK checks.
	if _, err := db.ExecContext(
		ctx, "PRAGMA foreign_keys = ON",
	); err != nil {
		return fmt.Errorf(
			"migration 5: could not re-enable foreign keys: %w",
			err,
		)
	}

	if _, err := db.ExecContext(
		ctx, "PRAGMA user_version = 5",
	); err != nil {
		return fmt.Errorf(
			"could not set user_version to 5: %w", err,
		)
	}

	logger.Info("migration 5 complete")

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

// backupDatabase copies the database file to a timestamped backup
// before running a destructive migration. Returns the backup path.
func backupDatabase(
	dbPath string, logger *slog.Logger,
) (string, error) {
	backupPath := dbPath + ".bak." + time.Now().Format("20060102")

	src, err := os.Open(dbPath)
	if err != nil {
		return "", fmt.Errorf(
			"could not open database for backup: %w", err,
		)
	}

	defer func() { _ = src.Close() }()

	dst, err := os.Create(backupPath)
	if err != nil {
		return "", fmt.Errorf(
			"could not create backup file: %w", err,
		)
	}

	defer func() { _ = dst.Close() }()

	if _, err := io.Copy(dst, src); err != nil {
		return "", fmt.Errorf(
			"could not copy database to backup: %w", err,
		)
	}

	logger.Info("database backup created", "path", backupPath)

	return backupPath, nil
}

// migration6MultiLibrary adds multi-library support: creates the
// libraries table, adds library_id FK to audio_files, rebuilds
// playlist_tracks with SET NULL FK and phantom metadata columns,
// and recreates the track_metadata VIEW with library_id.
//
// SAFETY: Hand-crafted SQL for schema migration.
func migration6MultiLibrary(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
	dbPath string,
) error {
	logger.Info("applying migration 6: multi-library support")

	// 1. Backup database BEFORE any changes (skip for in-memory DBs).
	if dbPath != "" && dbPath != ":memory:" {
		if _, err := backupDatabase(dbPath, logger); err != nil {
			return fmt.Errorf(
				"migration 6: backup failed: %w", err,
			)
		}
	}

	// 2. Read TOML config to get existing library directory.
	existingDir := readLibraryDirFromTOML(logger)

	// 3. Disable FK checks for table rebuild.
	// SAFETY: PRAGMA foreign_keys cannot run inside a transaction.
	if _, err := db.ExecContext(
		ctx, "PRAGMA foreign_keys = OFF",
	); err != nil {
		return fmt.Errorf(
			"migration 6: could not disable foreign keys: %w",
			err,
		)
	}

	// 4. Create libraries table.
	// SAFETY: Hand-crafted DDL for new table.
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS libraries (
			id         INTEGER PRIMARY KEY,
			name       TEXT NOT NULL,
			path       TEXT NOT NULL UNIQUE,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf(
			"migration 6: could not create libraries table: %w",
			err,
		)
	}

	// 5. Insert default library from TOML (if existingDir is not empty).
	var defaultLibID int64

	if existingDir != "" {
		libName := filepath.Base(existingDir)

		// SAFETY: Hand-crafted INSERT for migrated default library.
		result, err := db.ExecContext(ctx,
			"INSERT INTO libraries (name, path) VALUES (?, ?)",
			libName, existingDir,
		)
		if err != nil {
			return fmt.Errorf(
				"migration 6: could not insert default library: %w",
				err,
			)
		}

		defaultLibID, _ = result.LastInsertId()

		logger.Info("migrated existing library",
			"name", libName,
			"path", existingDir,
			"id", defaultLibID,
		)
	}

	// 6. Add library_id column to audio_files.
	// SAFETY: ALTER TABLE ADD COLUMN with dynamic DEFAULT for backfill.
	stmt := fmt.Sprintf(
		"ALTER TABLE audio_files ADD COLUMN library_id INTEGER NOT NULL DEFAULT %d",
		defaultLibID,
	)

	if _, err := db.ExecContext(ctx, stmt); err != nil {
		if !isDuplicateColumnErr(err) {
			return fmt.Errorf(
				"migration 6: could not add library_id column: %w",
				err,
			)
		}
	}

	// 7. Create index on library_id.
	// SAFETY: Hand-crafted index for FK performance.
	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_audio_files_library_id
			ON audio_files(library_id)
	`); err != nil {
		return fmt.Errorf(
			"migration 6: could not create library_id index: %w",
			err,
		)
	}

	// 8. Drop track_metadata VIEW before table rebuild.
	if _, err := db.ExecContext(
		ctx, "DROP VIEW IF EXISTS track_metadata",
	); err != nil {
		return fmt.Errorf(
			"migration 6: could not drop track_metadata VIEW: %w",
			err,
		)
	}

	// 9. Rebuild playlist_tracks for SET NULL FK + phantom columns.
	// SAFETY: Table rebuild — playlist_tracks changes to SET NULL,
	// queue_tracks keeps CASCADE (ephemeral, not rebuilt).

	// SAFETY: Hand-crafted DDL for rebuilt playlist_tracks.
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE playlist_tracks_new (
			id INTEGER PRIMARY KEY,
			playlist_id INTEGER NOT NULL,
			audio_file_id INTEGER,
			position INTEGER NOT NULL,
			phantom_title TEXT,
			phantom_artist TEXT,
			phantom_album TEXT,
			phantom_duration_ms INTEGER,
			phantom_genre TEXT,
			phantom_cover_art_path TEXT,
			FOREIGN KEY(playlist_id) REFERENCES playlists(id) ON DELETE CASCADE,
			FOREIGN KEY(audio_file_id) REFERENCES audio_files(id) ON DELETE SET NULL
		)
	`); err != nil {
		return fmt.Errorf(
			"migration 6: could not create playlist_tracks_new: %w",
			err,
		)
	}

	// Copy existing data (phantom columns get NULL).
	// SAFETY: Hand-crafted INSERT-SELECT for data migration.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO playlist_tracks_new (id, playlist_id, audio_file_id, position)
		SELECT id, playlist_id, audio_file_id, position FROM playlist_tracks
	`); err != nil {
		return fmt.Errorf(
			"migration 6: could not copy playlist_tracks data: %w",
			err,
		)
	}

	// Drop old table.
	if _, err := db.ExecContext(
		ctx, "DROP TABLE playlist_tracks",
	); err != nil {
		return fmt.Errorf(
			"migration 6: could not drop old playlist_tracks: %w",
			err,
		)
	}

	// Rename.
	if _, err := db.ExecContext(ctx,
		"ALTER TABLE playlist_tracks_new RENAME TO playlist_tracks",
	); err != nil {
		return fmt.Errorf(
			"migration 6: could not rename playlist_tracks_new: %w",
			err,
		)
	}

	// Recreate indexes.
	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_playlist_tracks_playlist_id
			ON playlist_tracks(playlist_id)
	`); err != nil {
		return fmt.Errorf(
			"migration 6: could not create playlist_id index: %w",
			err,
		)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_playlist_tracks_audio_file_id
			ON playlist_tracks(audio_file_id)
	`); err != nil {
		return fmt.Errorf(
			"migration 6: could not create audio_file_id index: %w",
			err,
		)
	}

	// 10. Backfill phantom metadata on existing playlist_tracks
	// from audio_files JOINs. Eager population per user decision.
	// SAFETY: Hand-crafted UPDATE-FROM-SELECT for phantom backfill.
	if _, err := db.ExecContext(ctx, `
		UPDATE playlist_tracks SET
			phantom_title = sub.title,
			phantom_artist = sub.artist,
			phantom_album = sub.album,
			phantom_duration_ms = sub.duration,
			phantom_genre = sub.genre,
			phantom_cover_art_path = sub.cover_art_path
		FROM (
			SELECT
				pt.id AS pt_id,
				COALESCE(r.name, '') AS title,
				COALESCE(ac.text, '') AS artist,
				COALESCE(rg.name, '') AS album,
				af.length_milliseconds AS duration,
				CAST(COALESCE(
					(SELECT GROUP_CONCAT(g.name, '||')
					 FROM recording_genres rg_sub
					 JOIN genres g ON rg_sub.genre_id = g.id
					 WHERE rg_sub.recording_id = r.id),
					''
				) AS TEXT) AS genre,
				COALESCE(ca.file_path, '') AS cover_art_path
			FROM playlist_tracks pt
			JOIN audio_files af ON pt.audio_file_id = af.id
			LEFT JOIN recordings r ON af.recording_id = r.id
			LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
			LEFT JOIN (
				SELECT recording_id, MIN(release_group_id) AS release_group_id
				FROM release_group_recordings
				GROUP BY recording_id
			) rgr ON r.id = rgr.recording_id
			LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
			LEFT JOIN cover_art ca ON rg.cover_art_id = ca.id
		) sub
		WHERE playlist_tracks.id = sub.pt_id
	`); err != nil {
		return fmt.Errorf(
			"migration 6: could not backfill phantom metadata: %w",
			err,
		)
	}

	// 11. Recreate track_metadata VIEW with library_id.
	// SAFETY: Hand-crafted VIEW recreation matching schema file.
	if _, err := db.ExecContext(ctx, `
		CREATE VIEW IF NOT EXISTS track_metadata AS
		SELECT
			af.id,
			af.file_path,
			af.length_milliseconds,
			COALESCE(r.name, '') AS title,
			COALESCE(ac.text, '') AS artist_name,
			r.track_number,
			r.disc_number,
			COALESCE(rg.name, '') AS album,
			CAST(COALESCE(
				(SELECT GROUP_CONCAT(g.name, '||')
				 FROM recording_genres rg_sub
				 JOIN genres g ON rg_sub.genre_id = g.id
				 WHERE rg_sub.recording_id = r.id),
				''
			) AS TEXT) AS genre,
			COALESCE(r.year, 0) AS year,
			COALESCE(r.composer, '') AS composer,
			COALESCE(ft.extension, '') AS file_type,
			af.sample_rate,
			af.bit_depth,
			af.channels,
			af.bitrate,
			af.file_size,
			af.library_id
		FROM audio_files af
		LEFT JOIN recordings r ON af.recording_id = r.id
		LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
		LEFT JOIN (
			SELECT recording_id,
				MIN(release_group_id) AS release_group_id
			FROM release_group_recordings
			GROUP BY recording_id
		) rgr ON r.id = rgr.recording_id
		LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
		LEFT JOIN file_types ft ON af.file_type_id = ft.id
	`); err != nil {
		return fmt.Errorf(
			"migration 6: could not recreate track_metadata VIEW: %w",
			err,
		)
	}

	// 12. Re-enable FK checks.
	if _, err := db.ExecContext(
		ctx, "PRAGMA foreign_keys = ON",
	); err != nil {
		return fmt.Errorf(
			"migration 6: could not re-enable foreign keys: %w",
			err,
		)
	}

	// 13. Remove DirectoryPath from TOML config (libraries table
	// is now the source of truth).
	if existingDir != "" {
		removeLibraryDirFromTOML(logger)
	}

	// 14. Set version.
	if _, err := db.ExecContext(
		ctx, "PRAGMA user_version = 6",
	); err != nil {
		return fmt.Errorf(
			"could not set user_version to 6: %w", err,
		)
	}

	logger.Info("migration 6 complete")

	return nil
}

// migration7PhantomFilePath adds the phantom_file_path column to
// playlist_tracks so that phantom entries can be automatically
// re-linked to audio_files after a library scan.
func migration7PhantomFilePath(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info(
		"applying migration 7: phantom_file_path column",
	)

	// SAFETY: ALTER TABLE ADD COLUMN for new nullable column.
	if _, err := db.ExecContext(ctx,
		`ALTER TABLE playlist_tracks
		 ADD COLUMN phantom_file_path TEXT`,
	); err != nil {
		if !isDuplicateColumnErr(err) {
			return fmt.Errorf(
				"migration 7: could not add column: %w",
				err,
			)
		}
	}

	if _, err := db.ExecContext(
		ctx, "PRAGMA user_version = 7",
	); err != nil {
		return fmt.Errorf(
			"could not set user_version to 7: %w", err,
		)
	}

	logger.Info("migration 7 complete")

	return nil
}

// readLibraryDirFromTOML reads the TOML config file and returns
// the Library.DirectoryPath value, or "" if not configured.
func readLibraryDirFromTOML(logger *slog.Logger) string {
	configDir, err := system.GetUserConfigDirPath()
	if err != nil {
		logger.Debug(
			"could not get config dir for TOML read",
			"err", err,
		)

		return ""
	}

	configPath := path.Join(configDir, "config.toml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		logger.Debug(
			"could not read config.toml",
			"path", configPath,
			"err", err,
		)

		return ""
	}

	// Minimal struct to extract only the Library.DirectoryPath field.
	var cfg struct {
		Library struct {
			DirectoryPath string `toml:"DirectoryPath"`
		} `toml:"Library"`
	}

	if _, err := toml.Decode(string(data), &cfg); err != nil {
		logger.Debug(
			"could not parse config.toml",
			"path", configPath,
			"err", err,
		)

		return ""
	}

	return cfg.Library.DirectoryPath
}

// removeLibraryDirFromTOML reads the TOML config, removes the
// Library.DirectoryPath field, and writes the config back. This
// ensures the libraries table is the sole source of truth after
// migration.
func removeLibraryDirFromTOML(logger *slog.Logger) {
	configDir, err := system.GetUserConfigDirPath()
	if err != nil {
		logger.Warn(
			"could not get config dir for TOML cleanup",
			"err", err,
		)

		return
	}

	configPath := path.Join(configDir, "config.toml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		logger.Warn(
			"could not read config.toml for cleanup",
			"path", configPath,
			"err", err,
		)

		return
	}

	// Parse the full config as a generic map to preserve all fields.
	var cfg map[string]any

	if _, err := toml.Decode(string(data), &cfg); err != nil {
		logger.Warn(
			"could not parse config.toml for cleanup",
			"err", err,
		)

		return
	}

	// Remove DirectoryPath from [Library] section.
	if lib, ok := cfg["Library"].(map[string]any); ok {
		delete(lib, "DirectoryPath")

		// If Library section is now empty, remove it entirely.
		if len(lib) == 0 {
			delete(cfg, "Library")
		}
	}

	// Write updated config back.
	out, err := toml.Marshal(cfg)
	if err != nil {
		logger.Warn(
			"could not marshal updated config.toml",
			"err", err,
		)

		return
	}

	if err := os.WriteFile(configPath, out, 0o644); err != nil {
		logger.Warn(
			"could not write updated config.toml",
			"path", configPath,
			"err", err,
		)

		return
	}

	logger.Info(
		"removed Library.DirectoryPath from config.toml",
		"path", configPath,
	)
}
