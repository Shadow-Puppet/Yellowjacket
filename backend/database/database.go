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

	"yellowjacket/backend/autotag"
	"yellowjacket/backend/database/sql/sqlcgen"
	"yellowjacket/backend/profiling"
	"yellowjacket/backend/system"
)

//go:generate go tool sqlc generate

//go:embed sql/schemas/*.sql
var schemas embed.FS

// DB wraps the SQLite database connection and queries.
//
// Two handles back a single database file.  db is the single-writer
// connection (MaxOpenConns 1) used for every write and every
// transaction.  readDB is a small multi-connection, query-only pool
// used for standalone reads.  Under WAL, readers run concurrently
// with the writer, so a long background write (index build, dump
// patch) no longer blocks interactive searches — the reason searches
// stalled for seconds was that the file was in rollback-journal mode
// with a single shared connection, so any writer locked out readers.
type DB struct {
	db     *sql.DB
	readDB *sql.DB
	Ctx    context.Context
	// Queries runs on the single-writer connection.  Use it for every
	// write and for any read that must observe an uncommitted write made
	// earlier in the same logical operation.
	Queries *sqlcgen.Queries
	// ReadQueries runs on the query-only WAL read pool, so standalone
	// reads proceed concurrently with a long background write instead of
	// queueing behind it on the single writer.  It observes only
	// committed data.  In tests (no read pool) it aliases Queries.
	ReadQueries *sqlcgen.Queries
	logger      *slog.Logger
}

// Data-source names.  modernc.org/sqlite only honours PRAGMAs passed
// as `_pragma=name(value)` — the mattn-style `_journal_mode=WAL`
// form is silently ignored, which is why WAL was never actually on.
const (
	writeDSNParams = "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	readDSNParams  = "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)" +
		"&_pragma=query_only(true)&_pragma=synchronous(NORMAL)" +
		"&_pragma=cache_size(-8000)&_pragma=mmap_size(67108864)"
	// readPoolConns bounds concurrent read connections.  A handful is
	// plenty for interactive search + art/lookup fan-out and keeps WAL
	// reader overhead small.
	readPoolConns = 4
)

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

	db, err := sql.Open("sqlite", sqliteDBFilePath+writeDSNParams)
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

	// Open a separate query-only read pool.  The write handle above
	// has already converted the file to WAL, so these connections read
	// a consistent snapshot concurrently with in-flight writes.
	readDB, err := sql.Open("sqlite", sqliteDBFilePath+readDSNParams)
	if err != nil {
		return nil, fmt.Errorf("could not open read pool: %w", err)
	}

	readDB.SetMaxOpenConns(readPoolConns)

	return &DB{
		db:          db,
		readDB:      readDB,
		Ctx:         dbCtx,
		Queries:     queries,
		ReadQueries: sqlcgen.New(readDB),
		logger:      logger,
	}, err
}

// reader returns the handle standalone reads should use: the
// query-only read pool when present, else the write handle (tests
// share one in-memory connection, which cannot be reopened).
func (d *DB) reader() *sql.DB {
	if d.readDB != nil {
		return d.readDB
	}

	return d.db
}

// BeginTx starts a new database transaction.
func (d *DB) BeginTx() (*sql.Tx, error) {
	return d.db.BeginTx(d.Ctx, nil)
}

// ExecContext executes a query without returning any rows.
func (d *DB) ExecContext(query string, args ...any) (sql.Result, error) {
	return d.db.ExecContext(d.Ctx, query, args...)
}

// QueryContext executes a query that returns rows.  Reads run on the
// query-only read pool so they proceed concurrently with writes under
// WAL instead of queueing behind the single writer connection.
func (d *DB) QueryContext(query string, args ...any) (*sql.Rows, error) {
	return d.reader().QueryContext(d.Ctx, query, args...)
}

// QueryContextWith executes a query that returns rows using a
// caller-supplied context instead of the DB's lifecycle context.
// This lets an individual query (e.g. a superseded search) be
// cancelled independently.  Like QueryContext it runs on the read
// pool.
func (d *DB) QueryContextWith(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.reader().QueryContext(ctx, query, args...)
}

// Logger returns the structured logger bound to this DB. Callers can
// use it to emit timing or diagnostic logs from query-adjacent code.
func (d *DB) Logger() *slog.Logger {
	return d.logger
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

	// Migration 8: rebuild FTS5 search_index with
	// contentless_delete=1 so individual rows can be deleted.
	if version < 8 {
		if err := migration8ContentlessDelete(
			ctx, db, logger,
		); err != nil {
			return err
		}
	}

	// Migration 9: add smart playlist columns to playlists.
	if version < 9 {
		if err := migration9SmartPlaylists(
			ctx, db, logger,
		); err != nil {
			return err
		}
	}

	if version < 10 {
		if err := migration10PlayHistory(
			ctx, db, logger,
		); err != nil {
			return err
		}
	}

	// Migration 11: explore_cache table for MusicBrainz/ListenBrainz
	// API response caching with TTL expiry and MBID lookups.
	if version < 11 {
		if err := migration11ExploreCache(
			ctx, db, logger,
		); err != nil {
			return err
		}
	}

	// Migration 12: explore_index + FTS5 for the popularity search
	// index.  Stores the top albums and tracks from the most popular
	// ListenBrainz artists for instant local search.
	if version < 12 { //nolint:mnd
		if err := migration12ExploreSearchIndex(
			ctx, db, logger,
		); err != nil {
			return err
		}
	}

	// Migration 13: add MusicBrainz ID columns to artists,
	// release_groups, and recordings for library↔explore linking.
	if version < 13 { //nolint:mnd
		if err := migration13MBIDColumns(
			ctx, db, logger,
		); err != nil {
			return err
		}
	}

	// Migration 14: add aliases column to explore_index and rebuild
	// the FTS5 virtual table with 3 searchable columns.
	if version < 14 { //nolint:mnd
		if err := migration14ExploreAliases(
			ctx, db, logger,
		); err != nil {
			return err
		}
	}

	// Migration 15: add in_library and is_similar columns to
	// explore_index for personalized search ranking.
	if version < 15 { //nolint:mnd
		if err := migration15PersonalizationColumns(
			ctx, db, logger,
		); err != nil {
			return err
		}
	}

	// Migration 16: artist_images table for multi-source artist photos.
	if version < 16 { //nolint:mnd
		if err := migration16ArtistImages(
			ctx, db, logger,
		); err != nil {
			return err
		}
	}

	if version < 17 { //nolint:mnd
		if err := migration17SimilarArtistMap(
			ctx, db, logger,
		); err != nil {
			return err
		}
	}

	if version < 18 {
		if err := migration18TrackCoverArt(
			ctx, db, logger,
		); err != nil {
			return err
		}
	}

	if version < 19 {
		if err := migration19TrackMBIDs(
			ctx, db, logger,
		); err != nil {
			return err
		}
	}

	if version < 20 {
		if err := migration20TrackRecordingMBID(
			ctx, db, logger,
		); err != nil {
			return err
		}
	}

	if version < 21 { //nolint:mnd
		logger.Info("applying migration 21: explore_index mbid-only index")

		if _, err := db.ExecContext(ctx, `
			CREATE INDEX IF NOT EXISTS idx_explore_index_mbid_only
			ON explore_index(mbid)
		`); err != nil {
			return fmt.Errorf("migration 21: create mbid-only index: %w", err)
		}

		if _, err := db.ExecContext(ctx,
			"PRAGMA user_version = 21",
		); err != nil {
			return fmt.Errorf("migration 21: set user_version: %w", err)
		}
	}

	if version < 22 { //nolint:mnd
		logger.Info("applying migration 22: replace composite index with UNIQUE(mbid)")

		// Remove any rows with empty MBIDs — they can't be looked up
		// and would violate the new UNIQUE(mbid) constraint.
		if _, err := db.ExecContext(ctx, `
			DELETE FROM explore_index WHERE mbid = ''
		`); err != nil {
			return fmt.Errorf("migration 22: delete empty mbids: %w", err)
		}

		// Drop the over-engineered composite — MBIDs are globally
		// unique, so entity_type in the key adds nothing.
		if _, err := db.ExecContext(ctx, `
			DROP INDEX IF EXISTS idx_explore_index_mbid
		`); err != nil {
			return fmt.Errorf("migration 22: drop composite index: %w", err)
		}

		// Drop the plain index from migration 21 and recreate as UNIQUE.
		if _, err := db.ExecContext(ctx, `
			DROP INDEX IF EXISTS idx_explore_index_mbid_only
		`); err != nil {
			return fmt.Errorf("migration 22: drop plain mbid index: %w", err)
		}

		if _, err := db.ExecContext(ctx, `
			CREATE UNIQUE INDEX IF NOT EXISTS idx_explore_index_mbid_only
			ON explore_index(mbid)
		`); err != nil {
			return fmt.Errorf("migration 22: create unique mbid index: %w", err)
		}

		if _, err := db.ExecContext(ctx,
			"PRAGMA user_version = 22",
		); err != nil {
			return fmt.Errorf("migration 22: set user_version: %w", err)
		}
	}

	if version < 23 { //nolint:mnd
		logger.Info("applying migration 23: search_clicks table")

		if _, err := db.ExecContext(ctx, `
			CREATE TABLE IF NOT EXISTS search_clicks (
				query        TEXT NOT NULL,
				entity_mbid  TEXT NOT NULL,
				entity_type  TEXT NOT NULL,
				click_count  INTEGER NOT NULL DEFAULT 1,
				last_clicked DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				PRIMARY KEY (query, entity_mbid)
			)
		`); err != nil {
			return fmt.Errorf("migration 23: create search_clicks: %w", err)
		}

		if _, err := db.ExecContext(ctx, `
			CREATE INDEX IF NOT EXISTS idx_search_clicks_query
			ON search_clicks(query)
		`); err != nil {
			return fmt.Errorf("migration 23: create query index: %w", err)
		}

		if _, err := db.ExecContext(ctx,
			"PRAGMA user_version = 23",
		); err != nil {
			return fmt.Errorf("migration 23: set user_version: %w", err)
		}
	}

	if version < 24 { //nolint:mnd
		logger.Info("applying migration 24: explore_index listener_count + duration columns")

		if _, err := db.ExecContext(ctx, `
			ALTER TABLE explore_index ADD COLUMN listener_count INTEGER NOT NULL DEFAULT 0
		`); err != nil {
			// Column may already exist from a partial migration.
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("migration 24: add listener_count: %w", err)
			}
		}

		if _, err := db.ExecContext(ctx, `
			ALTER TABLE explore_index ADD COLUMN duration INTEGER NOT NULL DEFAULT 0
		`); err != nil {
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("migration 24: add duration: %w", err)
			}
		}

		if _, err := db.ExecContext(ctx,
			"PRAGMA user_version = 24",
		); err != nil {
			return fmt.Errorf("migration 24: set user_version: %w", err)
		}
	}

	if version < 25 { //nolint:mnd
		logger.Info("applying migration 25: explore_index duration column")

		if _, err := db.ExecContext(ctx, `
			ALTER TABLE explore_index ADD COLUMN duration INTEGER NOT NULL DEFAULT 0
		`); err != nil {
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("migration 25: add duration: %w", err)
			}
		}

		if _, err := db.ExecContext(ctx,
			"PRAGMA user_version = 25",
		); err != nil {
			return fmt.Errorf("migration 25: set user_version: %w", err)
		}
	}

	if version < 26 { //nolint:mnd
		logger.Info("applying migration 26: comprehensive explore schema overhaul")

		// Nuke the existing index — we're changing the schema enough
		// that a clean rebuild is simpler than trying to migrate in place.
		if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS explore_index_fts`); err != nil {
			return fmt.Errorf("migration 26: drop fts: %w", err)
		}

		if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS explore_index`); err != nil {
			return fmt.Errorf("migration 26: drop explore_index: %w", err)
		}

		// Create the new explore_index with all typed columns.
		// No more extra_json — every field that matters has its own column.
		if _, err := db.ExecContext(ctx, `
			CREATE TABLE explore_index (
				id                       INTEGER PRIMARY KEY AUTOINCREMENT,
				entity_type              TEXT NOT NULL,
				mbid                     TEXT NOT NULL,
				title                    TEXT NOT NULL,
				artist_name              TEXT NOT NULL,
				artist_mbid              TEXT NOT NULL,
				aliases                  TEXT NOT NULL DEFAULT '',

				-- Popularity signals (from LB popularity API, uncapped).
				popularity               INTEGER NOT NULL DEFAULT 0,
				listener_count           INTEGER NOT NULL DEFAULT 0,

				-- Recording-specific fields.
				duration                 INTEGER NOT NULL DEFAULT 0,
				caa_release_mbid         TEXT NOT NULL DEFAULT '',
				release_name             TEXT NOT NULL DEFAULT '',

				-- Release-group-specific fields.
				primary_type             TEXT NOT NULL DEFAULT '',
				secondary_types          TEXT NOT NULL DEFAULT '',
				release_date             TEXT NOT NULL DEFAULT '',

				-- Artist-specific fields.
				artist_type              TEXT NOT NULL DEFAULT '',
				country                  TEXT NOT NULL DEFAULT '',
				disambiguation           TEXT NOT NULL DEFAULT '',
				sort_name                TEXT NOT NULL DEFAULT '',

				-- Personalization flags.
				in_library               INTEGER NOT NULL DEFAULT 0,
				is_similar               INTEGER NOT NULL DEFAULT 0,

				-- Cross-reference to local library tables.  NULL when the
				-- entity has no corresponding row in the library.
				local_artist_id          INTEGER,
				local_release_group_id   INTEGER,
				local_recording_id       INTEGER,

				-- Set to 1 by indexOneArtist after fetching the full
				-- discography (release groups + recordings).  Used by
				-- indexedArtistMBIDs() so the AddFromCache organic-growth
				-- path doesn't shadow artists from later tier 2/3 runs.
				discog_fetched           INTEGER NOT NULL DEFAULT 0,

				-- Schema version — lets us mark rows as stale after schema changes.
				schema_version           INTEGER NOT NULL DEFAULT 1,

				UNIQUE(mbid)
			)
		`); err != nil {
			return fmt.Errorf("migration 26: create explore_index: %w", err)
		}

		if _, err := db.ExecContext(ctx, `
			CREATE INDEX idx_explore_index_artist_mbid
			ON explore_index(artist_mbid, entity_type, popularity DESC)
		`); err != nil {
			return fmt.Errorf("migration 26: create artist_mbid index: %w", err)
		}

		if _, err := db.ExecContext(ctx, `
			CREATE INDEX idx_explore_index_entity_pop
			ON explore_index(entity_type, popularity DESC)
		`); err != nil {
			return fmt.Errorf("migration 26: create entity_pop index: %w", err)
		}

		// FTS5 virtual table for text search.
		if _, err := db.ExecContext(ctx, `
			CREATE VIRTUAL TABLE explore_index_fts USING fts5(
				title, artist_name, aliases,
				content='explore_index',
				content_rowid='id'
			)
		`); err != nil {
			return fmt.Errorf("migration 26: create fts: %w", err)
		}

		// Triggers to keep FTS in sync with the main table.
		if _, err := db.ExecContext(ctx, `
			CREATE TRIGGER explore_index_ai AFTER INSERT ON explore_index BEGIN
				INSERT INTO explore_index_fts(rowid, title, artist_name, aliases)
				VALUES (new.id, new.title, new.artist_name, new.aliases);
			END
		`); err != nil {
			return fmt.Errorf("migration 26: create ai trigger: %w", err)
		}

		if _, err := db.ExecContext(ctx, `
			CREATE TRIGGER explore_index_ad AFTER DELETE ON explore_index BEGIN
				INSERT INTO explore_index_fts(explore_index_fts, rowid, title, artist_name, aliases)
				VALUES ('delete', old.id, old.title, old.artist_name, old.aliases);
			END
		`); err != nil {
			return fmt.Errorf("migration 26: create ad trigger: %w", err)
		}

		if _, err := db.ExecContext(ctx, `
			CREATE TRIGGER explore_index_au AFTER UPDATE ON explore_index BEGIN
				INSERT INTO explore_index_fts(explore_index_fts, rowid, title, artist_name, aliases)
				VALUES ('delete', old.id, old.title, old.artist_name, old.aliases);
				INSERT INTO explore_index_fts(rowid, title, artist_name, aliases)
				VALUES (new.id, new.title, new.artist_name, new.aliases);
			END
		`); err != nil {
			return fmt.Errorf("migration 26: create au trigger: %w", err)
		}

		// Clear the tier metadata so the next build repopulates everything.
		if _, err := db.ExecContext(ctx, `DELETE FROM explore_index_meta`); err != nil {
			return fmt.Errorf("migration 26: clear meta: %w", err)
		}

		if _, err := db.ExecContext(ctx,
			"PRAGMA user_version = 26",
		); err != nil {
			return fmt.Errorf("migration 26: set user_version: %w", err)
		}
	}

	if version < 27 { //nolint:mnd
		logger.Info(
			"applying migration 27: split explore_cache into http_cache and artist_metadata",
		)

		// Create the new tables (no-op if schemas/*.sql already created them).
		if _, err := db.ExecContext(ctx, `
			CREATE TABLE IF NOT EXISTS artist_metadata (
				mbid       TEXT NOT NULL,
				source     TEXT NOT NULL,
				data       BLOB NOT NULL,
				fetched_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				PRIMARY KEY (mbid, source)
			)
		`); err != nil {
			return fmt.Errorf("migration 27: create artist_metadata: %w", err)
		}

		if _, err := db.ExecContext(ctx, `
			CREATE INDEX IF NOT EXISTS idx_artist_metadata_mbid
			ON artist_metadata(mbid)
		`); err != nil {
			return fmt.Errorf("migration 27: create artist_metadata index: %w", err)
		}

		if _, err := db.ExecContext(ctx, `
			CREATE TABLE IF NOT EXISTS http_cache (
				url_key    TEXT PRIMARY KEY,
				response   BLOB NOT NULL,
				expires_at DATETIME NOT NULL,
				entity_mbid TEXT NOT NULL DEFAULT '',
				entity_type TEXT NOT NULL DEFAULT ''
			)
		`); err != nil {
			return fmt.Errorf("migration 27: create http_cache: %w", err)
		}

		if _, err := db.ExecContext(ctx, `
			CREATE INDEX IF NOT EXISTS idx_http_cache_expires
			ON http_cache(expires_at)
		`); err != nil {
			return fmt.Errorf("migration 27: create http_cache index: %w", err)
		}

		// Only migrate existing data if explore_cache exists (not a fresh install).
		var exploreCacheExists bool
		{
			row, err := db.QueryContext(ctx,
				"SELECT 1 FROM sqlite_master WHERE type='table' AND name='explore_cache'",
			)
			if err == nil {
				if row.Next() {
					exploreCacheExists = true
				}

				_ = row.Close()
			}
		}

		if exploreCacheExists {
			// Migrate long-lived sources into artist_metadata.
			for _, src := range []string{"audiodb", "fanart", "wikidata-p18", "wikipedia-lead"} {
				if _, err := db.ExecContext(ctx, `
					INSERT OR IGNORE INTO artist_metadata (mbid, source, data, fetched_at)
					SELECT substr(url_key, ?+1), ?, response, COALESCE(expires_at, CURRENT_TIMESTAMP)
					FROM explore_cache
					WHERE url_key LIKE ?
				`, len(src)+1, src, src+":%"); err != nil {
					return fmt.Errorf("migration 27: migrate %s: %w", src, err)
				}
			}

			// Migrate remaining (short-lived) entries into http_cache.
			if _, err := db.ExecContext(ctx, `
				INSERT OR IGNORE INTO http_cache (url_key, response, expires_at, entity_mbid, entity_type)
				SELECT url_key, response, expires_at,
				       COALESCE(mbid, ''), COALESCE(entity_type, '')
				FROM explore_cache
			`); err != nil {
				return fmt.Errorf("migration 27: migrate http_cache: %w", err)
			}

			// Drop the old table.
			if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS explore_cache`); err != nil {
				return fmt.Errorf("migration 27: drop explore_cache: %w", err)
			}
		}

		if _, err := db.ExecContext(ctx,
			"PRAGMA user_version = 27",
		); err != nil {
			return fmt.Errorf("migration 27: set user_version: %w", err)
		}
	}

	if version < 28 { //nolint:mnd
		logger.Info(
			"applying migration 28: repair broken similar_artist_map data from multi-seed labs bug",
		)

		// The multi-seed POST form of the labs similar-artists endpoint
		// returns mis-grouped results — each seed ends up with a random
		// subset of the shared result pool (1-2 artists for most seeds,
		// hundreds for a few).  Clear the bad rows and invalidate the
		// tier4 timestamp so the next index build refetches per-seed.
		if _, err := db.ExecContext(ctx,
			"DELETE FROM similar_artist_map",
		); err != nil {
			return fmt.Errorf("migration 28: clear similar_artist_map: %w", err)
		}

		// Invalidate the tier4 build timestamp so the next startup
		// triggers a Tier 4 rebuild.  Also clear is_similar markers
		// so they get recomputed.
		if _, err := db.ExecContext(ctx,
			"DELETE FROM explore_index_meta WHERE key = 'tier4_built'",
		); err != nil {
			// Not fatal — the meta table might not exist yet.
			logger.Warn(
				"migration 28: clear tier4_built failed (ok on fresh install)",
				"error",
				err,
			)
		}

		if _, err := db.ExecContext(ctx,
			"UPDATE explore_index SET is_similar = 0 WHERE is_similar = 1",
		); err != nil {
			// Not fatal — explore_index might not exist yet on a
			// fresh install where migration 26 just ran.
			logger.Warn("migration 28: clear is_similar failed", "error", err)
		}

		if _, err := db.ExecContext(ctx,
			"PRAGMA user_version = 28",
		); err != nil {
			return fmt.Errorf("migration 28: set user_version: %w", err)
		}
	}

	if version < 29 { //nolint:mnd
		logger.Info(
			"applying migration 29: discog_fetched column to track full indexer pipeline coverage",
		)

		// Add a discog_fetched column to explore_index.  When set to 1
		// on an artist row, the indexer's fetchTopRecordings/
		// fetchTopReleaseGroups pipeline has run for that artist.
		// AddFromCache (the frontend-visit organic-growth path) does
		// NOT set this flag — it only writes the artist row plus
		// browse-result release groups, so recordings are missing.
		//
		// indexedArtistMBIDs() filters by discog_fetched=1, so artists
		// who only got their row from AddFromCache will still be
		// processed by Tier 2/3 and have their full discography fetched
		// (including recordings).
		if _, err := db.ExecContext(ctx, `
			ALTER TABLE explore_index
			ADD COLUMN discog_fetched INTEGER NOT NULL DEFAULT 0
		`); err != nil {
			// May fail if migration runs against a fresh schema (column
			// will be created by the schema file instead).  Don't bail.
			logger.Warn(
				"migration 29: add discog_fetched column failed (ok if fresh)",
				"error",
				err,
			)
		}

		// Backfill: any artist with at least 5 recordings was almost
		// certainly hit by fetchTopRecordings (the floor is 5).  Use
		// this as a heuristic to mark existing data as "discog fetched"
		// so the migration is non-disruptive — only the broken
		// AddFromCache-only artists get re-indexed.
		if _, err := db.ExecContext(ctx, `
			UPDATE explore_index
			SET discog_fetched = 1
			WHERE entity_type = 'artist'
			  AND mbid IN (
			    SELECT artist_mbid
			    FROM explore_index
			    WHERE entity_type = 'recording'
			    GROUP BY artist_mbid
			    HAVING COUNT(*) >= 5
			  )
		`); err != nil {
			logger.Warn("migration 29: backfill discog_fetched failed", "error", err)
		}

		if _, err := db.ExecContext(ctx,
			"PRAGMA user_version = 29",
		); err != nil {
			return fmt.Errorf("migration 29: set user_version: %w", err)
		}
	}

	if version < 30 { //nolint:mnd
		logger.Info(
			"applying migration 30: invalidate MB browse-releases cache for recording MBID fix",
		)

		// Earlier versions of convertRelease used the MusicBrainz
		// track MBID instead of the recording MBID for MBTrack.MBID.
		// Tracks and recordings have distinct MBIDs in MB, and the
		// local library tags files with the recording MBID, so the
		// library-status indicator on album detail pages was always
		// showing "not in library" for cached results.  Clear the
		// http_cache entries for MB browse-releases so the next
		// visit refetches with the fixed converter.
		if _, err := db.ExecContext(ctx,
			"DELETE FROM http_cache WHERE url_key LIKE 'mb:browse:releases:%'",
		); err != nil {
			// Not fatal — cache might not exist on fresh installs.
			logger.Warn("migration 30: clear browse-releases cache failed", "error", err)
		}

		if _, err := db.ExecContext(ctx,
			"PRAGMA user_version = 30",
		); err != nil {
			return fmt.Errorf("migration 30: set user_version: %w", err)
		}
	}

	if version < 31 { //nolint:mnd
		if err := migration31TagStatus(ctx, db, logger); err != nil {
			return err
		}
	}

	if version < 32 { //nolint:mnd
		if err := migration32TaggingItems(ctx, db, logger); err != nil {
			return err
		}
	}

	if version < 33 { //nolint:mnd
		if err := migration33AutotagWarning(ctx, db, logger); err != nil {
			return err
		}
	}

	if version < 34 { //nolint:mnd
		if err := migration34FolderBasedGroupKey(ctx, db, logger); err != nil {
			return err
		}
	}

	if version < 35 { //nolint:mnd
		if err := migration35OriginalYear(ctx, db, logger); err != nil {
			return err
		}
	}

	if version < 36 { //nolint:mnd
		if err := migration36ClearedAt(ctx, db, logger); err != nil {
			return err
		}
	}

	if version < 37 { //nolint:mnd
		if err := migration37ExploreFTSDiacritics(ctx, db, logger); err != nil {
			return err
		}
	}

	if version < 38 { //nolint:mnd
		if err := migration38TaggingCandidates(ctx, db, logger); err != nil {
			return err
		}
	}

	if version < 39 { //nolint:mnd
		if err := migration39LyricsIndex(ctx, db, logger); err != nil {
			return err
		}
	}

	if version < 40 { //nolint:mnd
		if err := migration40ExploreExactMatchIndexes(ctx, db, logger); err != nil {
			return err
		}
	}

	if version < 41 { //nolint:mnd
		if err := migration41ExploreChampionFTS(ctx, db, logger); err != nil {
			return err
		}
	}

	if version < 42 { //nolint:mnd
		if err := migration42ReleaseToRG(ctx, db, logger); err != nil {
			return err
		}
	}

	if version < 43 { //nolint:mnd
		if err := migration43MergeArtistCredits(ctx, db, logger); err != nil {
			return err
		}
	}

	if version < 44 { //nolint:mnd
		if err := migration44ExploreCAAReleaseIndex(ctx, db, logger); err != nil {
			return err
		}
	}

	if version < 45 { //nolint:mnd
		if err := migration45Analyze(ctx, db, logger); err != nil {
			return err
		}
	}

	if version < 46 { //nolint:mnd
		if err := migration46SmartSnapshot(ctx, db, logger); err != nil {
			return err
		}
	}

	return nil
}

// migration46SmartSnapshot adds the smart_snapshot_at column to the
// playlists table. Smart playlists now materialize their evaluated
// membership into playlist_tracks and only re-evaluate on demand; the
// timestamp records when that snapshot was last taken (NULL means the
// playlist has never been materialized, so it is backfilled on first
// open).
func migration46SmartSnapshot(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info("applying migration 46: smart playlist snapshot column")

	if _, err := db.ExecContext(ctx,
		`ALTER TABLE playlists
		 ADD COLUMN smart_snapshot_at DATETIME`,
	); err != nil {
		if !isDuplicateColumnErr(err) {
			return fmt.Errorf(
				"migration 46: could not add smart_snapshot_at column: %w",
				err,
			)
		}
	}

	if _, err := db.ExecContext(
		ctx, "PRAGMA user_version = 46",
	); err != nil {
		return fmt.Errorf(
			"migration 46: set user_version: %w", err,
		)
	}

	logger.Info("migration 46 complete")

	return nil
}

// migration45Analyze runs ANALYZE so SQLite's query planner has real
// table/index statistics.  Without stats the planner guesses from row
// counts alone and mis-chose indexes on the ~2M-row explore_index — e.g.
// the top-result parent-release lookup scanned all 400k release_group
// rows via idx_explore_index_entity_pop instead of seeking the new
// idx_explore_caa_release, costing seconds per search.  ANALYZE populates
// sqlite_stat1 (a one-time ~1.5s scan) and the planner then picks the
// right index for that query and every other query on these large tables.
func migration45Analyze(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info("applying migration 45: ANALYZE for query planner statistics")

	if _, err := db.ExecContext(ctx, "ANALYZE"); err != nil {
		return fmt.Errorf("migration 45: analyze: %w", err)
	}

	if _, err := db.ExecContext(ctx, "PRAGMA user_version = 45"); err != nil {
		return fmt.Errorf("migration 45: set user_version: %w", err)
	}

	logger.Info("migration 45 complete")

	return nil
}

// migration44ExploreCAAReleaseIndex adds a partial index on
// caa_release_mbid so the top-result resolver's parent-release-group
// lookup (SearchIndex.ReleaseGroupMBIDsForCAAReleaseMBIDs) seeks the
// index instead of scanning every release_group row in explore_index
// (~150k) on the hot search path.  The index is partial, mirroring the
// query's own filter (entity_type = 'release_group' AND
// caa_release_mbid is non-empty), so it stays small and covers exactly the
// rows that lookup can match.  Without it, a generic query whose top
// results include recordings with cover art (e.g. "big") spends
// seconds in this scan.
func migration44ExploreCAAReleaseIndex(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info("applying migration 44: explore caa_release_mbid index")

	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_explore_caa_release
		ON explore_index(caa_release_mbid)
		WHERE entity_type = 'release_group' AND caa_release_mbid != ''
	`); err != nil {
		return fmt.Errorf("migration 44: create caa_release index: %w", err)
	}

	if _, err := db.ExecContext(ctx, "PRAGMA user_version = 44"); err != nil {
		return fmt.Errorf("migration 44: set user_version: %w", err)
	}

	logger.Info("migration 44 complete")

	return nil
}

// migration43MergeArtistCredits repairs artist rows that were created
// from full credit strings.  Before the scanner resolved a track's
// primary artist, a credit like "Lana Del Rey ft. Sean Lennon" was
// stored as its own artists row and stamped with the primary artist's
// single MBID — so one MusicBrainz artist fanned out into many rows that
// shared an MBID, and the explore index (last-write-wins per MBID) then
// displayed a featured-credit string as the artist's name.
//
// This collapses every set of artists rows that share an MBID into the
// one "clean" member (a name with no featuring clause), repoints the
// artist_credit_artist links, deletes the redundant rows, and refreshes
// the explore index's artist titles from the survivors.  Clusters with
// no clean member (all names carry a marker) are left untouched.
func migration43MergeArtistCredits(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info("applying migration 43: merge credit-string artists")

	// Map each redundant artist row to the clean canonical row for its
	// MBID.  "Clean" = a name carrying no featuring marker; the lowest
	// id among those is the canonical survivor.
	if _, err := db.ExecContext(ctx, `
		CREATE TEMP TABLE artist_merge_map AS
		SELECT a.id AS dirty_id, canon.canon_id AS canon_id
		FROM artists a
		JOIN (
			SELECT mbid, MIN(id) AS canon_id
			FROM artists
			WHERE mbid IS NOT NULL AND mbid != ''
			  AND lower(name) NOT LIKE '% feat %'
			  AND lower(name) NOT LIKE '% feat. %'
			  AND lower(name) NOT LIKE '% featuring %'
			  AND lower(name) NOT LIKE '% ft %'
			  AND lower(name) NOT LIKE '% ft. %'
			GROUP BY mbid
		) canon ON canon.mbid = a.mbid
		WHERE a.id != canon.canon_id
	`); err != nil {
		return fmt.Errorf("migration 43: build merge map: %w", err)
	}

	// Drop links that would collide with an existing (canonical, credit)
	// link after repointing — the unique index would otherwise reject
	// the UPDATE.
	if _, err := db.ExecContext(ctx, `
		DELETE FROM artist_credit_artist
		WHERE id IN (
			SELECT aca.id
			FROM artist_credit_artist aca
			JOIN artist_merge_map m ON m.dirty_id = aca.artist_id
			WHERE EXISTS (
				SELECT 1 FROM artist_credit_artist keep
				WHERE keep.artist_id = m.canon_id
				  AND keep.credit_id = aca.credit_id
			)
		)
	`); err != nil {
		return fmt.Errorf("migration 43: prune colliding links: %w", err)
	}

	// Repoint surviving links to the canonical artist.
	if _, err := db.ExecContext(ctx, `
		UPDATE artist_credit_artist
		SET artist_id = (
			SELECT canon_id FROM artist_merge_map
			WHERE dirty_id = artist_credit_artist.artist_id
		)
		WHERE artist_id IN (SELECT dirty_id FROM artist_merge_map)
	`); err != nil {
		return fmt.Errorf("migration 43: repoint links: %w", err)
	}

	// Remove the now-orphaned credit-string artist rows.
	if _, err := db.ExecContext(ctx, `
		DELETE FROM artists WHERE id IN (SELECT dirty_id FROM artist_merge_map)
	`); err != nil {
		return fmt.Errorf("migration 43: delete merged artists: %w", err)
	}

	// Refresh explore-index artist rows from the surviving library
	// artists so their (previously clobbered) titles show the clean
	// name.  The AFTER UPDATE trigger keeps explore_index_fts in sync.
	// Only rows backed by a library artist are touched; dump-only rows
	// are left alone.
	if _, err := db.ExecContext(ctx, `
		UPDATE explore_index
		SET title = (
			    SELECT name FROM artists
			    WHERE artists.mbid = explore_index.mbid ORDER BY id LIMIT 1),
		    artist_name = (
			    SELECT name FROM artists
			    WHERE artists.mbid = explore_index.mbid ORDER BY id LIMIT 1),
		    local_artist_id = (
			    SELECT id FROM artists
			    WHERE artists.mbid = explore_index.mbid ORDER BY id LIMIT 1)
		WHERE entity_type = 'artist'
		  AND EXISTS (SELECT 1 FROM artists WHERE artists.mbid = explore_index.mbid)
	`); err != nil {
		return fmt.Errorf("migration 43: refresh explore titles: %w", err)
	}

	if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS artist_merge_map"); err != nil {
		return fmt.Errorf("migration 43: drop temp table: %w", err)
	}

	if _, err := db.ExecContext(ctx, "PRAGMA user_version = 43"); err != nil {
		return fmt.Errorf("migration 43: set user_version: %w", err)
	}

	logger.Info("migration 43 complete")

	return nil
}

// migration42ReleaseToRG creates the release_to_rg mapping table: for
// every release under an indexed release group, which release-group it
// belongs to.  It is populated from the canonical dump during a full
// import (the mapping is otherwise in-memory only and discarded).  The
// incremental-dump popularity refresh uses it to roll per-release listen
// deltas up to their release group, so album popularity stays fresh
// without any API call.
func migration42ReleaseToRG(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info("applying migration 42: release_to_rg table")

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS release_to_rg (
			release_mbid TEXT PRIMARY KEY,
			rg_mbid      TEXT NOT NULL
		) WITHOUT ROWID
	`); err != nil {
		return fmt.Errorf("migration 42: create release_to_rg: %w", err)
	}

	if _, err := db.ExecContext(ctx,
		"PRAGMA user_version = 42",
	); err != nil {
		return fmt.Errorf("migration 42: set user_version: %w", err)
	}

	logger.Info("migration 42 complete")

	return nil
}

// migration41ExploreChampionFTS creates the "champion" full-text index:
// a second external-content FTS5 over explore_index that holds only the
// high-popularity / owned rows.  Short generic prefixes ("the", "a")
// match hundreds of thousands of rows in the full index, and the
// popularity-blended ORDER BY must score every one of them — seconds of
// work.  Routing those queries at the champion index instead scores only
// the ~90k rows that could plausibly win, cutting the query from seconds
// to tens of milliseconds.  The table is created empty here; the search
// index populates it at runtime (see SearchIndex.RebuildChampionIndex)
// because the row set derives from popularity, which changes as the
// index is (re)built.
func migration41ExploreChampionFTS(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info("applying migration 41: explore champion FTS")

	if _, err := db.ExecContext(ctx, `
		CREATE VIRTUAL TABLE IF NOT EXISTS explore_champion_fts USING fts5(
			title, artist_name, aliases,
			content='explore_index',
			content_rowid='id',
			tokenize='unicode61 remove_diacritics 2'
		)
	`); err != nil {
		return fmt.Errorf("migration 41: create champion fts: %w", err)
	}

	if _, err := db.ExecContext(ctx, "PRAGMA user_version = 41"); err != nil {
		return fmt.Errorf("migration 41: set user_version: %w", err)
	}

	logger.Info("migration 41 complete")

	return nil
}

// migration39LyricsIndex creates the contentless FTS5 lyrics_index
// (see lyrics_index.sql) and back-populates it from any recordings
// that already have embedded lyrics, so lyric search works on
// existing libraries without waiting for a rescan.
func migration39LyricsIndex(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info("applying migration 39: lyrics_index FTS")

	if _, err := db.ExecContext(ctx, `
		CREATE VIRTUAL TABLE IF NOT EXISTS lyrics_index USING fts5(
			lyrics,
			content='',
			contentless_delete=1,
			tokenize='unicode61 remove_diacritics 2'
		)
	`); err != nil {
		return fmt.Errorf("migration 39: create lyrics_index: %w", err)
	}

	// Back-populate from recordings that already carry lyrics.  The
	// rowid is the recording id so it stays stable across rebuilds.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO lyrics_index(rowid, lyrics)
		SELECT id, lyrics
		FROM recordings
		WHERE lyrics IS NOT NULL AND lyrics != ''
	`); err != nil {
		return fmt.Errorf("migration 39: populate lyrics_index: %w", err)
	}

	if _, err := db.ExecContext(ctx, "PRAGMA user_version = 39"); err != nil {
		return fmt.Errorf("migration 39: set user_version: %w", err)
	}

	logger.Info("migration 39 complete")

	return nil
}

// migration40ExploreExactMatchIndexes adds partial expression indexes
// on LOWER(title) and LOWER(artist_name) so the interactive top-result
// resolver's exact-match lookup (SearchIndex.ExactMatches) seeks the
// index instead of scanning all ~240k explore_index rows on every
// keystroke.  The indexes are partial (WHERE popularity > 0) because
// that lookup always filters on popularity, keeping them small; the
// UNION-of-equalities query shape in ExactMatches is what lets SQLite
// use them (an OR across the two columns forces a scan instead).
func migration40ExploreExactMatchIndexes(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info("applying migration 40: explore exact-match indexes")

	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_explore_title_lower
		ON explore_index(LOWER(title))
		WHERE popularity > 0
	`); err != nil {
		return fmt.Errorf("migration 40: create title index: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_explore_artist_lower
		ON explore_index(LOWER(artist_name))
		WHERE popularity > 0
	`); err != nil {
		return fmt.Errorf("migration 40: create artist index: %w", err)
	}

	if _, err := db.ExecContext(ctx, "PRAGMA user_version = 40"); err != nil {
		return fmt.Errorf("migration 40: set user_version: %w", err)
	}

	logger.Info("migration 40 complete")

	return nil
}

// migration38TaggingCandidates creates the tagging_candidates table —
// a durable per-group store for the scored candidate list so it is
// computed once and reused across restarts instead of re-hitting
// MusicBrainz every session (see tagging_candidates.sql).  A plain
// CREATE TABLE IF NOT EXISTS is safe on both fresh and existing DBs.
func migration38TaggingCandidates(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info("applying migration 38: tagging_candidates")

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS tagging_candidates (
		  group_key   TEXT PRIMARY KEY,
		  candidates  TEXT NOT NULL,
		  computed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		  FOREIGN KEY(group_key) REFERENCES tagging_items(group_key) ON DELETE CASCADE
		)
	`); err != nil {
		return fmt.Errorf("migration 38: create tagging_candidates: %w", err)
	}

	if _, err := db.ExecContext(ctx, "PRAGMA user_version = 38"); err != nil {
		return fmt.Errorf("migration 38: set user_version: %w", err)
	}

	logger.Info("migration 38 complete")

	return nil
}

// migration36ClearedAt adds tagging_items.cleared_at — a nullable
// timestamp set when the user invokes "clear completed entries".
// Cleared rows stay in the table (so a re-scan doesn't resurrect
// them as pending) but get filtered from the review queue.
func migration36ClearedAt(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info("applying migration 36: tagging_items.cleared_at")

	if _, err := db.ExecContext(
		ctx,
		`ALTER TABLE tagging_items ADD COLUMN cleared_at DATETIME`,
	); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("migration 36: add cleared_at: %w", err)
		}

		logger.Warn("migration 36: cleared_at already present (ok if fresh)", "err", err)
	}

	if _, err := db.ExecContext(ctx, "PRAGMA user_version = 36"); err != nil {
		return fmt.Errorf("migration 36: set user_version: %w", err)
	}

	logger.Info("migration 36 complete")

	return nil
}

// migration37ExploreFTSDiacritics rebuilds explore_index_fts with the
// "unicode61 remove_diacritics 2" tokeniser so accented queries match
// their unaccented forms (e.g. "beyonce" finds "Beyoncé"), matching the
// library search_index tokeniser.  The original table (migration 26)
// was created with the default tokeniser, which does not fold
// diacritics.
//
// Because explore_index_fts is an external-content table over
// explore_index, the rebuild repopulates from the existing content
// rows — no data loss and no need to re-run the expensive tiered index
// build.
func migration37ExploreFTSDiacritics(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info("applying migration 37: explore_index_fts diacritic folding")

	// Drop the sync triggers and the FTS table, then recreate both.
	// The triggers must go first — they reference the FTS table.
	stmts := []string{
		`DROP TRIGGER IF EXISTS explore_index_ai`,
		`DROP TRIGGER IF EXISTS explore_index_ad`,
		`DROP TRIGGER IF EXISTS explore_index_au`,
		`DROP TABLE IF EXISTS explore_index_fts`,
		`CREATE VIRTUAL TABLE explore_index_fts USING fts5(
			title, artist_name, aliases,
			content='explore_index',
			content_rowid='id',
			tokenize='unicode61 remove_diacritics 2'
		)`,
		`CREATE TRIGGER explore_index_ai AFTER INSERT ON explore_index BEGIN
			INSERT INTO explore_index_fts(rowid, title, artist_name, aliases)
			VALUES (new.id, new.title, new.artist_name, new.aliases);
		END`,
		`CREATE TRIGGER explore_index_ad AFTER DELETE ON explore_index BEGIN
			INSERT INTO explore_index_fts(explore_index_fts, rowid, title, artist_name, aliases)
			VALUES ('delete', old.id, old.title, old.artist_name, old.aliases);
		END`,
		`CREATE TRIGGER explore_index_au AFTER UPDATE ON explore_index BEGIN
			INSERT INTO explore_index_fts(explore_index_fts, rowid, title, artist_name, aliases)
			VALUES ('delete', old.id, old.title, old.artist_name, old.aliases);
			INSERT INTO explore_index_fts(rowid, title, artist_name, aliases)
			VALUES (new.id, new.title, new.artist_name, new.aliases);
		END`,
		// Repopulate the FTS index from the content table.
		`INSERT INTO explore_index_fts(explore_index_fts) VALUES('rebuild')`,
	}

	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migration 37: %w", err)
		}
	}

	if _, err := db.ExecContext(ctx, "PRAGMA user_version = 37"); err != nil {
		return fmt.Errorf("migration 37: set user_version: %w", err)
	}

	logger.Info("migration 37 complete")

	return nil
}

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

	// Copy all data. Columns are listed explicitly so later schema
	// additions (e.g. migration 13's mbid column) don't break this
	// migration when it runs on a fresh DB where CREATE TABLE IF NOT
	// EXISTS has already materialized the current schema.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO release_groups_new
			(id, name, cover_art_id, album_artist_credit_id,
			 year, total_tracks, total_discs)
		SELECT id, name, cover_art_id, album_artist_credit_id,
			year, total_tracks, total_discs
		FROM release_groups
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

// migration8ContentlessDelete rebuilds the FTS5 search_index with
// contentless_delete=1 so that individual rows can be deleted.
// This is a prerequisite for inline tag edit → DB sync in Phase 16.
//
// SAFETY: Hand-crafted SQL for FTS5 schema migration.
func migration8ContentlessDelete(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info(
		"applying migration 8: rebuilding FTS5 search_index with contentless_delete=1",
	)

	// Drop the old contentless FTS5 table (content='' only).
	if _, err := db.ExecContext(
		ctx, `DROP TABLE IF EXISTS search_index`,
	); err != nil {
		return fmt.Errorf(
			"migration 8: could not drop search_index: %w", err,
		)
	}

	// Recreate with contentless_delete=1 added.
	// SAFETY: Must match backend/database/sql/schemas/search_index.sql exactly.
	if _, err := db.ExecContext(ctx, `
		CREATE VIRTUAL TABLE IF NOT EXISTS search_index USING fts5(
			file_path,
			title,
			artist,
			album,
			content='',
			contentless_delete=1,
			tokenize='unicode61 remove_diacritics 2'
		)
	`); err != nil {
		return fmt.Errorf(
			"migration 8: could not recreate search_index: %w", err,
		)
	}

	// Repopulate from track_metadata VIEW.
	// SAFETY: FTS5 INSERT from VIEW; no user input.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO search_index(rowid, file_path, title, artist, album)
		SELECT id, file_path, title, artist_name, album
		FROM track_metadata
	`); err != nil {
		return fmt.Errorf(
			"migration 8: could not repopulate search_index: %w", err,
		)
	}

	if _, err := db.ExecContext(
		ctx, "PRAGMA user_version = 8",
	); err != nil {
		return fmt.Errorf(
			"migration 8: could not set user_version: %w", err,
		)
	}

	logger.Info("migration 8 complete")

	return nil
}

// migration9SmartPlaylists adds the is_smart and smart_rules
// columns to the playlists table for smart playlist support.
func migration9SmartPlaylists(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info(
		"applying migration 9: smart playlist columns",
	)

	if _, err := db.ExecContext(ctx,
		`ALTER TABLE playlists
		 ADD COLUMN is_smart INTEGER NOT NULL DEFAULT 0`,
	); err != nil {
		if !isDuplicateColumnErr(err) {
			return fmt.Errorf(
				"migration 9: could not add is_smart column: %w",
				err,
			)
		}
	}

	if _, err := db.ExecContext(ctx,
		`ALTER TABLE playlists
		 ADD COLUMN smart_rules TEXT`,
	); err != nil {
		if !isDuplicateColumnErr(err) {
			return fmt.Errorf(
				"migration 9: could not add smart_rules column: %w",
				err,
			)
		}
	}

	if _, err := db.ExecContext(
		ctx, "PRAGMA user_version = 9",
	); err != nil {
		return fmt.Errorf(
			"could not set user_version to 9: %w", err,
		)
	}

	logger.Info("migration 9 complete")

	return nil
}

// migration10PlayHistory adds play history tracking:
// - play_history table for timestamped play log
// - play_count and last_played columns on audio_files
// - Recreates track_metadata VIEW to expose the new columns.
func migration10PlayHistory(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info(
		"applying migration 10: play history tracking",
	)

	// 1. Create play_history table.
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS play_history (
			id INTEGER PRIMARY KEY,
			audio_file_id INTEGER NOT NULL,
			played_at DATETIME NOT NULL DEFAULT (datetime('now')),
			FOREIGN KEY(audio_file_id) REFERENCES audio_files(id) ON DELETE CASCADE
		)`,
	); err != nil {
		return fmt.Errorf(
			"migration 10: could not create play_history table: %w",
			err,
		)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_play_history_audio_file_id
		ON play_history(audio_file_id)`,
	); err != nil {
		return fmt.Errorf(
			"migration 10: could not create play_history index: %w",
			err,
		)
	}

	// 2. Add play_count and last_played columns to audio_files.
	if _, err := db.ExecContext(ctx,
		`ALTER TABLE audio_files
		 ADD COLUMN play_count INTEGER NOT NULL DEFAULT 0`,
	); err != nil {
		if !isDuplicateColumnErr(err) {
			return fmt.Errorf(
				"migration 10: could not add play_count column: %w",
				err,
			)
		}
	}

	if _, err := db.ExecContext(ctx,
		`ALTER TABLE audio_files
		 ADD COLUMN last_played DATETIME`,
	); err != nil {
		if !isDuplicateColumnErr(err) {
			return fmt.Errorf(
				"migration 10: could not add last_played column: %w",
				err,
			)
		}
	}

	// 3. Recreate track_metadata VIEW to include play_count and last_played.
	if _, err := db.ExecContext(
		ctx, "DROP VIEW IF EXISTS track_metadata",
	); err != nil {
		return fmt.Errorf(
			"migration 10: could not drop track_metadata VIEW: %w",
			err,
		)
	}

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
			af.library_id,
			af.play_count,
			af.last_played
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
		LEFT JOIN file_types ft ON af.file_type_id = ft.id`,
	); err != nil {
		return fmt.Errorf(
			"migration 10: could not create track_metadata VIEW: %w",
			err,
		)
	}

	if _, err := db.ExecContext(
		ctx, "PRAGMA user_version = 10",
	); err != nil {
		return fmt.Errorf(
			"could not set user_version to 10: %w", err,
		)
	}

	logger.Info("migration 10 complete")

	return nil
}

// migration11ExploreCache creates the explore_cache table for
// MusicBrainz and ListenBrainz API response caching. The table
// stores raw JSON keyed by URL with TTL-based expiry and optional
// MBID columns for future autotagging lookups.
func migration11ExploreCache(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info(
		"applying migration 11: explore_cache table",
	)

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS explore_cache (
			url_key     TEXT PRIMARY KEY,
			response    TEXT NOT NULL,
			mbid        TEXT,
			entity_type TEXT,
			expires_at  DATETIME NOT NULL,
			created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf(
			"migration 11: could not create explore_cache table: %w",
			err,
		)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_explore_cache_expires
		ON explore_cache(expires_at)
	`); err != nil {
		return fmt.Errorf(
			"migration 11: could not create expires index: %w",
			err,
		)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_explore_cache_mbid
		ON explore_cache(mbid)
	`); err != nil {
		return fmt.Errorf(
			"migration 11: could not create mbid index: %w",
			err,
		)
	}

	if _, err := db.ExecContext(
		ctx, "PRAGMA user_version = 11",
	); err != nil {
		return fmt.Errorf(
			"could not set user_version to 11: %w", err,
		)
	}

	logger.Info("migration 11 complete")

	return nil
}

// migration12ExploreSearchIndex creates the explore_index table,
// the FTS5 virtual table for full-text search, sync triggers, and
// the explore_index_meta table for build tracking.
func migration12ExploreSearchIndex(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info("applying migration 12: explore search index")

	// Content table — slim denormalized rows for search.
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS explore_index (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			entity_type TEXT NOT NULL,
			mbid        TEXT NOT NULL,
			title       TEXT NOT NULL,
			artist_name TEXT NOT NULL,
			artist_mbid TEXT NOT NULL,
			popularity  INTEGER NOT NULL DEFAULT 0,
			extra_json  TEXT
		)
	`); err != nil {
		return fmt.Errorf("migration 12: create explore_index: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE UNIQUE INDEX IF NOT EXISTS idx_explore_index_mbid
		ON explore_index(entity_type, mbid)
	`); err != nil {
		return fmt.Errorf("migration 12: create mbid index: %w", err)
	}

	// FTS5 virtual table backed by the content table.
	if _, err := db.ExecContext(ctx, `
		CREATE VIRTUAL TABLE IF NOT EXISTS explore_index_fts USING fts5(
			title, artist_name,
			content='explore_index',
			content_rowid='id'
		)
	`); err != nil {
		return fmt.Errorf("migration 12: create FTS5 table: %w", err)
	}

	// Triggers to keep FTS in sync.
	if _, err := db.ExecContext(ctx, `
		CREATE TRIGGER IF NOT EXISTS explore_index_ai AFTER INSERT ON explore_index BEGIN
			INSERT INTO explore_index_fts(rowid, title, artist_name)
			VALUES (new.id, new.title, new.artist_name);
		END
	`); err != nil {
		return fmt.Errorf("migration 12: create insert trigger: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE TRIGGER IF NOT EXISTS explore_index_ad AFTER DELETE ON explore_index BEGIN
			INSERT INTO explore_index_fts(explore_index_fts, rowid, title, artist_name)
			VALUES ('delete', old.id, old.title, old.artist_name);
		END
	`); err != nil {
		return fmt.Errorf("migration 12: create delete trigger: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE TRIGGER IF NOT EXISTS explore_index_au AFTER UPDATE ON explore_index BEGIN
			INSERT INTO explore_index_fts(explore_index_fts, rowid, title, artist_name)
			VALUES ('delete', old.id, old.title, old.artist_name);
			INSERT INTO explore_index_fts(rowid, title, artist_name)
			VALUES (new.id, new.title, new.artist_name);
		END
	`); err != nil {
		return fmt.Errorf("migration 12: create update trigger: %w", err)
	}

	// Metadata table for build tracking.
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS explore_index_meta (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("migration 12: create meta table: %w", err)
	}

	if _, err := db.ExecContext(
		ctx, "PRAGMA user_version = 12",
	); err != nil {
		return fmt.Errorf("could not set user_version to 12: %w", err)
	}

	logger.Info("migration 12 complete")

	return nil
}

// migration13MBIDColumns adds MusicBrainz ID columns to artists,
// release_groups, and recordings for linking local library entities
// to MusicBrainz/ListenBrainz explore data.
func migration13MBIDColumns(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info("applying migration 13: MusicBrainz ID columns")

	alterStmts := []struct {
		table  string
		column string
	}{
		{"artists", "mbid"},
		{"release_groups", "mbid"},
		{"recordings", "mbid"},
	}

	for _, s := range alterStmts {
		stmt := fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN %s TEXT", s.table, s.column,
		)

		if _, err := db.ExecContext(ctx, stmt); err != nil {
			// Column may already exist from a partial migration.
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("migration 13: alter %s: %w", s.table, err)
			}
		}
	}

	// Partial indexes for MBID lookups (only index non-NULL rows).
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_artists_mbid ON artists(mbid) WHERE mbid IS NOT NULL",
		"CREATE INDEX IF NOT EXISTS idx_release_groups_mbid ON release_groups(mbid) WHERE mbid IS NOT NULL",
		"CREATE INDEX IF NOT EXISTS idx_recordings_mbid ON recordings(mbid) WHERE mbid IS NOT NULL",
	}

	for _, stmt := range indexes {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migration 13: create index: %w", err)
		}
	}

	if _, err := db.ExecContext(
		ctx, "PRAGMA user_version = 13",
	); err != nil {
		return fmt.Errorf("could not set user_version to 13: %w", err)
	}

	logger.Info("migration 13 complete")

	return nil
}

// migration14ExploreAliases adds an aliases column to explore_index
// and rebuilds the FTS5 virtual table with three searchable columns
// (title, artist_name, aliases) for alias-aware search.
func migration14ExploreAliases(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info("applying migration 14: explore index aliases + FTS5 rebuild")

	// Add aliases column to content table.
	if _, err := db.ExecContext(ctx,
		"ALTER TABLE explore_index ADD COLUMN aliases TEXT DEFAULT ''",
	); err != nil {
		if !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("migration 14: alter explore_index: %w", err)
		}
	}

	// Drop old triggers.
	for _, name := range []string{
		"explore_index_ai", "explore_index_ad", "explore_index_au",
	} {
		if _, err := db.ExecContext(ctx,
			"DROP TRIGGER IF EXISTS "+name,
		); err != nil {
			return fmt.Errorf("migration 14: drop trigger %s: %w", name, err)
		}
	}

	// Drop and recreate FTS5 with 3 columns.
	if _, err := db.ExecContext(ctx,
		"DROP TABLE IF EXISTS explore_index_fts",
	); err != nil {
		return fmt.Errorf("migration 14: drop FTS5: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE VIRTUAL TABLE explore_index_fts USING fts5(
			title, artist_name, aliases,
			content='explore_index',
			content_rowid='id'
		)
	`); err != nil {
		return fmt.Errorf("migration 14: create FTS5: %w", err)
	}

	// Recreate triggers with 3 columns.
	if _, err := db.ExecContext(ctx, `
		CREATE TRIGGER explore_index_ai AFTER INSERT ON explore_index BEGIN
			INSERT INTO explore_index_fts(rowid, title, artist_name, aliases)
			VALUES (new.id, new.title, new.artist_name, new.aliases);
		END
	`); err != nil {
		return fmt.Errorf("migration 14: create insert trigger: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE TRIGGER explore_index_ad AFTER DELETE ON explore_index BEGIN
			INSERT INTO explore_index_fts(explore_index_fts, rowid, title, artist_name, aliases)
			VALUES ('delete', old.id, old.title, old.artist_name, old.aliases);
		END
	`); err != nil {
		return fmt.Errorf("migration 14: create delete trigger: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE TRIGGER explore_index_au AFTER UPDATE ON explore_index BEGIN
			INSERT INTO explore_index_fts(explore_index_fts, rowid, title, artist_name, aliases)
			VALUES ('delete', old.id, old.title, old.artist_name, old.aliases);
			INSERT INTO explore_index_fts(rowid, title, artist_name, aliases)
			VALUES (new.id, new.title, new.artist_name, new.aliases);
		END
	`); err != nil {
		return fmt.Errorf("migration 14: create update trigger: %w", err)
	}

	// Rebuild FTS5 index from existing content table rows.
	if _, err := db.ExecContext(ctx,
		"INSERT INTO explore_index_fts(explore_index_fts) VALUES ('rebuild')",
	); err != nil {
		return fmt.Errorf("migration 14: rebuild FTS5: %w", err)
	}

	if _, err := db.ExecContext(
		ctx, "PRAGMA user_version = 14",
	); err != nil {
		return fmt.Errorf("could not set user_version to 14: %w", err)
	}

	// Clear the index build timestamp so the next build populates aliases.
	_, _ = db.ExecContext(ctx,
		"DELETE FROM explore_index_meta WHERE key IN ('tier1_built', 'discog_built')",
	)

	logger.Info("migration 14 complete")

	return nil
}

// migration15PersonalizationColumns adds in_library and is_similar
// columns to explore_index for personalized search ranking.
func migration15PersonalizationColumns(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info("applying migration 15: personalization columns")

	for _, col := range []string{"in_library", "is_similar"} {
		stmt := fmt.Sprintf(
			"ALTER TABLE explore_index ADD COLUMN %s INTEGER NOT NULL DEFAULT 0", col,
		)

		if _, err := db.ExecContext(ctx, stmt); err != nil {
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("migration 15: alter explore_index: %w", err)
			}
		}
	}

	// Backfill in_library for artists already in the library.
	if _, err := db.ExecContext(ctx, `
		UPDATE explore_index SET in_library = 1
		WHERE entity_type = 'artist'
		AND mbid IN (SELECT mbid FROM artists WHERE mbid IS NOT NULL AND mbid != '')
	`); err != nil {
		logger.Warn("migration 15: backfill in_library artists", "error", err)
	}

	// Backfill in_library for release groups already in the library.
	if _, err := db.ExecContext(ctx, `
		UPDATE explore_index SET in_library = 1
		WHERE entity_type = 'release_group'
		AND mbid IN (SELECT mbid FROM release_groups WHERE mbid IS NOT NULL AND mbid != '')
	`); err != nil {
		logger.Warn("migration 15: backfill in_library release_groups", "error", err)
	}

	// Clear discog_built so the next index build populates these flags.
	_, _ = db.ExecContext(ctx,
		"DELETE FROM explore_index_meta WHERE key = 'discog_built'",
	)

	if _, err := db.ExecContext(
		ctx, "PRAGMA user_version = 15",
	); err != nil {
		return fmt.Errorf("could not set user_version to 15: %w", err)
	}

	logger.Info("migration 15 complete")

	return nil
}

// migration16ArtistImages creates the artist_images table for
// storing multiple artist photos from multiple sources, with
// thumbnail generation for the primary image.
func migration16ArtistImages(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info("applying migration 16: artist_images table")

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS artist_images (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			artist_mbid TEXT NOT NULL,
			source      TEXT NOT NULL,
			source_url  TEXT NOT NULL,
			file_path   TEXT NOT NULL,
			is_primary  INTEGER NOT NULL DEFAULT 0,
			sort_order  INTEGER NOT NULL DEFAULT 0,
			width       INTEGER,
			height      INTEGER,
			file_size   INTEGER,
			created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("migration 16: create artist_images: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_artist_images_mbid
		ON artist_images(artist_mbid)
	`); err != nil {
		return fmt.Errorf("migration 16: create mbid index: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE UNIQUE INDEX IF NOT EXISTS idx_artist_images_source
		ON artist_images(artist_mbid, source, source_url)
	`); err != nil {
		return fmt.Errorf("migration 16: create source index: %w", err)
	}

	if _, err := db.ExecContext(
		ctx, "PRAGMA user_version = 16",
	); err != nil {
		return fmt.Errorf("could not set user_version to 16: %w", err)
	}

	logger.Info("migration 16 complete")

	return nil
}

func migration17SimilarArtistMap(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info("applying migration 17: similar_artist_map table")

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS similar_artist_map (
			source_artist_mbid  TEXT NOT NULL,
			similar_artist_mbid TEXT NOT NULL,
			similar_artist_name TEXT NOT NULL,
			score               INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (source_artist_mbid, similar_artist_mbid)
		)
	`); err != nil {
		return fmt.Errorf("migration 17: create similar_artist_map: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_similar_artist_map_source
		ON similar_artist_map(source_artist_mbid)
	`); err != nil {
		return fmt.Errorf("migration 17: create source index: %w", err)
	}

	if _, err := db.ExecContext(ctx,
		"PRAGMA user_version = 17",
	); err != nil {
		return fmt.Errorf("could not set user_version to 17: %w", err)
	}

	logger.Info("migration 17 complete")

	return nil
}

// migration18TrackCoverArt recreates the track_metadata VIEW to
// include cover_art_path via a JOIN to the cover_art table.
func migration18TrackCoverArt(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info("applying migration 18: track_metadata cover_art_path")

	if _, err := db.ExecContext(
		ctx, "DROP VIEW IF EXISTS track_metadata",
	); err != nil {
		return fmt.Errorf("migration 18: drop view: %w", err)
	}

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
			af.library_id,
			af.play_count,
			af.last_played,
			COALESCE(ca.file_path, '') AS cover_art_path
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
		LEFT JOIN cover_art ca ON rg.cover_art_id = ca.id
		LEFT JOIN file_types ft ON af.file_type_id = ft.id
	`); err != nil {
		return fmt.Errorf("migration 18: create view: %w", err)
	}

	if _, err := db.ExecContext(ctx,
		"PRAGMA user_version = 18",
	); err != nil {
		return fmt.Errorf("could not set user_version to 18: %w", err)
	}

	logger.Info("migration 18 complete")

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

// migration19TrackMBIDs recreates the track_metadata VIEW to include
// artist_mbid and release_group_mbid columns via the relational
// chain: recording → artist_credit → artist_credit_artist → artist.
func migration19TrackMBIDs(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info("applying migration 19: track_metadata MBID columns")

	if _, err := db.ExecContext(
		ctx, "DROP VIEW IF EXISTS track_metadata",
	); err != nil {
		return fmt.Errorf("migration 19: drop view: %w", err)
	}

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
			af.library_id,
			af.play_count,
			af.last_played,
			COALESCE(ca.file_path, '') AS cover_art_path,
			COALESCE(a.mbid, '') AS artist_mbid,
			COALESCE(rg.mbid, '') AS release_group_mbid,
			COALESCE(r.mbid, '') AS recording_mbid
		FROM audio_files af
		LEFT JOIN recordings r ON af.recording_id = r.id
		LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
		LEFT JOIN artist_credit_artist aca ON aca.credit_id = ac.id
		LEFT JOIN artists a ON a.id = aca.artist_id
		LEFT JOIN (
			SELECT recording_id,
				MIN(release_group_id) AS release_group_id
			FROM release_group_recordings
			GROUP BY recording_id
		) rgr ON r.id = rgr.recording_id
		LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
		LEFT JOIN cover_art ca ON rg.cover_art_id = ca.id
		LEFT JOIN file_types ft ON af.file_type_id = ft.id
	`); err != nil {
		return fmt.Errorf("migration 19: create view: %w", err)
	}

	if _, err := db.ExecContext(ctx,
		"PRAGMA user_version = 19",
	); err != nil {
		return fmt.Errorf("could not set user_version to 19: %w", err)
	}

	logger.Info("migration 19 complete")

	return nil
}

// migration20TrackRecordingMBID recreates the track_metadata VIEW to
// add the recording_mbid column (missed in migration 19).
func migration20TrackRecordingMBID(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info("applying migration 20: track_metadata recording_mbid")

	if _, err := db.ExecContext(
		ctx, "DROP VIEW IF EXISTS track_metadata",
	); err != nil {
		return fmt.Errorf("migration 20: drop view: %w", err)
	}

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
			af.library_id,
			af.play_count,
			af.last_played,
			COALESCE(ca.file_path, '') AS cover_art_path,
			COALESCE(a.mbid, '') AS artist_mbid,
			COALESCE(rg.mbid, '') AS release_group_mbid,
			COALESCE(r.mbid, '') AS recording_mbid
		FROM audio_files af
		LEFT JOIN recordings r ON af.recording_id = r.id
		LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
		LEFT JOIN artist_credit_artist aca ON aca.credit_id = ac.id
		LEFT JOIN artists a ON a.id = aca.artist_id
		LEFT JOIN (
			SELECT recording_id,
				MIN(release_group_id) AS release_group_id
			FROM release_group_recordings
			GROUP BY recording_id
		) rgr ON r.id = rgr.recording_id
		LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
		LEFT JOIN cover_art ca ON rg.cover_art_id = ca.id
		LEFT JOIN file_types ft ON af.file_type_id = ft.id
	`); err != nil {
		return fmt.Errorf("migration 20: create view: %w", err)
	}

	// Purge stale ListenBrainz top-recordings cache entries that
	// were written before the caaReleaseMbid field was added to
	// the LBTopRecording struct.  Without this, cached entries
	// render without cover art thumbnails in the top tracks section.
	if _, err := db.ExecContext(ctx,
		"DELETE FROM explore_cache WHERE url_key LIKE 'lb:top-recordings:%'",
	); err != nil {
		logger.Warn("migration 20: could not purge stale top-recordings cache", "err", err)
		// Non-fatal — entries will expire naturally via TTL.
	}

	if _, err := db.ExecContext(ctx,
		"PRAGMA user_version = 20",
	); err != nil {
		return fmt.Errorf("could not set user_version to 20: %w", err)
	}

	logger.Info("migration 20 complete")

	return nil
}

// migration31TagStatus adds the tag_status column to audio_files,
// indexes the "untagged" slice for the pending-count badge, and
// backfills rows whose recording already carries an MBID as
// `user_confirmed`.  Everything else stays at the `untagged`
// default.  The column-level CHECK constraint is added inline with
// the ALTER TABLE — SQLite supports column constraints in ADD
// COLUMN, so existing DBs pick it up too.
func migration31TagStatus(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info("applying migration 31: tag_status column")

	if _, err := db.ExecContext(ctx, `
		ALTER TABLE audio_files
		ADD COLUMN tag_status TEXT NOT NULL DEFAULT 'untagged'
		CHECK(tag_status IN (
		  'untagged', 'auto_matched', 'user_confirmed', 'user_skipped_permanent'
		))
	`); err != nil && !isDuplicateColumnErr(err) {
		return fmt.Errorf("migration 31: add tag_status: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_audio_files_tag_status_untagged
		ON audio_files(library_id) WHERE tag_status = 'untagged'
	`); err != nil {
		return fmt.Errorf("migration 31: create index: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		UPDATE audio_files
		SET tag_status = 'user_confirmed'
		WHERE tag_status = 'untagged'
		  AND recording_id IN (
		    SELECT id FROM recordings
		    WHERE mbid IS NOT NULL AND mbid != ''
		  )
	`); err != nil {
		return fmt.Errorf("migration 31: backfill tag_status: %w", err)
	}

	if _, err := db.ExecContext(ctx,
		"PRAGMA user_version = 31",
	); err != nil {
		return fmt.Errorf("migration 31: set user_version: %w", err)
	}

	logger.Info("migration 31 complete")

	return nil
}

// migration32TaggingItems creates the tagging_items table and adds
// the group_key column to audio_files, then backfills both from the
// current `audio_files` / `recordings` / `release_groups` state.
// The Go-side autotag.GroupKey helper is the single source of truth
// for the key format (keeps the hash algorithm decoupled from SQL).
//
// SAFETY: Hand-crafted ALTER TABLE + CREATE TABLE + streaming
// backfill inside a single transaction.
func migration32TaggingItems(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info("applying migration 32: tagging_items + group_key")

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS tagging_items (
		  group_key               TEXT PRIMARY KEY,
		  library_id              INTEGER NOT NULL,
		  track_count             INTEGER NOT NULL DEFAULT 0,
		  album_name              TEXT NOT NULL DEFAULT '',
		  album_artist            TEXT NOT NULL DEFAULT '',
		  disc_number             INTEGER NOT NULL DEFAULT 0,
		  best_match_release_mbid TEXT,
		  score                   REAL,
		  last_checked_at         DATETIME,
		  status                  TEXT NOT NULL DEFAULT 'pending'
		    CHECK(status IN ('pending', 'matched', 'confirmed', 'skipped')),
		  created_at              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		  FOREIGN KEY(library_id) REFERENCES libraries(id)
		)
	`); err != nil {
		return fmt.Errorf("migration 32: create tagging_items: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_tagging_items_library_status
		ON tagging_items(library_id, status)
	`); err != nil {
		return fmt.Errorf("migration 32: create library_status index: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_tagging_items_status_pending
		ON tagging_items(library_id) WHERE status = 'pending'
	`); err != nil {
		return fmt.Errorf("migration 32: create pending index: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		ALTER TABLE audio_files
		ADD COLUMN group_key TEXT NOT NULL DEFAULT ''
	`); err != nil && !isDuplicateColumnErr(err) {
		return fmt.Errorf("migration 32: add group_key: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_audio_files_group_key
		ON audio_files(group_key) WHERE group_key != ''
	`); err != nil {
		return fmt.Errorf("migration 32: create group_key index: %w", err)
	}

	if err := backfillGroupKeys(ctx, db, logger); err != nil {
		return fmt.Errorf("migration 32: backfill group_key: %w", err)
	}

	if err := aggregateTaggingItems(ctx, db, logger); err != nil {
		return fmt.Errorf("migration 32: aggregate tagging_items: %w", err)
	}

	if _, err := db.ExecContext(ctx,
		"PRAGMA user_version = 32",
	); err != nil {
		return fmt.Errorf("migration 32: set user_version: %w", err)
	}

	logger.Info("migration 32 complete")

	return nil
}

// backfillGroupKeys streams existing audio_files rows in batches of
// ~500 and writes the computed group_key back via a single UPDATE
// per row inside one transaction.  It joins to release_groups for
// the album name and recordings for the disc number; both fall back
// to the zero value when absent.
func backfillGroupKeys(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	const batchSize = 500

	type row struct {
		id         int64
		libraryID  int64
		filePath   string
		discNumber int64
	}

	for {
		// Each pass reads the next N rows with group_key still empty;
		// once updated, they drop out of the filter, so no OFFSET
		// bookkeeping is needed.
		rows, err := db.QueryContext(ctx, `
			SELECT af.id, af.library_id, af.file_path,
			       COALESCE(r.disc_number, 0)
			FROM audio_files af
			LEFT JOIN recordings r ON af.recording_id = r.id
			WHERE af.group_key = ''
			ORDER BY af.id
			LIMIT ?
		`, batchSize)
		if err != nil {
			return fmt.Errorf("select batch: %w", err)
		}

		batch := make([]row, 0, batchSize)

		for rows.Next() {
			var r row
			if scanErr := rows.Scan(
				&r.id, &r.libraryID, &r.filePath, &r.discNumber,
			); scanErr != nil {
				_ = rows.Close()

				return fmt.Errorf("scan row: %w", scanErr)
			}

			batch = append(batch, r)
		}

		if closeErr := rows.Close(); closeErr != nil {
			return fmt.Errorf("close rows: %w", closeErr)
		}

		if len(batch) == 0 {
			break
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin tx: %w", err)
		}

		for _, r := range batch {
			key := autotag.GroupKey(
				r.libraryID, r.filePath, int(r.discNumber),
			)
			if _, err := tx.ExecContext(ctx,
				`UPDATE audio_files SET group_key = ? WHERE id = ?`,
				key, r.id,
			); err != nil {
				_ = tx.Rollback()

				return fmt.Errorf("update row %d: %w", r.id, err)
			}
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit tx: %w", err)
		}

		logger.Debug(
			"migration 32: backfilled group_key batch", "count", len(batch),
		)

		if len(batch) < batchSize {
			break
		}
	}

	return nil
}

// aggregateTaggingItems populates tagging_items from the now-
// populated audio_files.group_key, one row per (group_key,
// library_id) pair.  Status defaults to `confirmed` when every
// track in the group already has tag_status `user_confirmed`,
// otherwise `pending`.
func aggregateTaggingItems(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	result, err := db.ExecContext(ctx, `
		INSERT INTO tagging_items (
		  group_key, library_id, track_count,
		  album_name, album_artist, disc_number, status
		)
		SELECT
		  af.group_key,
		  af.library_id,
		  COUNT(*) AS track_count,
		  COALESCE(MAX(rg.name), '') AS album_name,
		  COALESCE(MAX(ac.text), '') AS album_artist,
		  COALESCE(MAX(r.disc_number), 0) AS disc_number,
		  CASE WHEN SUM(CASE WHEN af.tag_status = 'user_confirmed' THEN 0 ELSE 1 END) = 0
		       THEN 'confirmed' ELSE 'pending' END AS status
		FROM audio_files af
		LEFT JOIN recordings r ON af.recording_id = r.id
		LEFT JOIN release_group_recordings rgr ON rgr.recording_id = r.id
		LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
		LEFT JOIN artist_credit ac ON rg.album_artist_credit_id = ac.id
		WHERE af.group_key != ''
		GROUP BY af.group_key, af.library_id
		ON CONFLICT(group_key) DO NOTHING
	`)
	if err != nil {
		return fmt.Errorf("insert aggregates: %w", err)
	}

	if n, rowsErr := result.RowsAffected(); rowsErr == nil {
		logger.Debug(
			"migration 32: aggregated tagging_items rows",
			"count", n,
		)
	}

	return nil
}

// migration33AutotagWarning adds the per-library flag that records
// whether the user has seen (and dismissed) the first-time autotag
// apply warning.  Zero means "still warn"; one means acknowledged.
func migration33AutotagWarning(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info("applying migration 33: libraries.autotag_warning_acked")

	if _, err := db.ExecContext(ctx, `
		ALTER TABLE libraries
		ADD COLUMN autotag_warning_acked INTEGER NOT NULL DEFAULT 0
	`); err != nil && !isDuplicateColumnErr(err) {
		return fmt.Errorf("migration 33: add column: %w", err)
	}

	if _, err := db.ExecContext(ctx,
		"PRAGMA user_version = 33",
	); err != nil {
		return fmt.Errorf("migration 33: set user_version: %w", err)
	}

	logger.Info("migration 33 complete")

	return nil
}

// migration35OriginalYear adds release_groups.original_year (the
// release-group's MusicBrainz first-release-date year) and rebuilds
// the track_metadata view so its "year" column prefers the original
// release year over the file-tag year.  This makes a 1973 album
// show as 1973 in the tracklist and smart-playlist year rules even
// when the user owns the 2010 remaster.  release_year is added as
// a separate view column for callers that need the file-tag year.
func migration35OriginalYear(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info("applying migration 35: release_groups.original_year + view rebuild")

	if _, err := db.ExecContext(
		ctx,
		`ALTER TABLE release_groups ADD COLUMN original_year INTEGER`,
	); err != nil {
		// Tolerate duplicate-column on re-run / fresh-DB schema race.
		if !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("migration 35: add original_year: %w", err)
		}

		logger.Warn("migration 35: original_year already present (ok if fresh)", "err", err)
	}

	// Drop and recreate the track_metadata view so its year column
	// picks up the new fallback chain.  CREATE VIEW IF NOT EXISTS
	// in the schema file is a no-op once the view exists, so we have
	// to do this explicitly here for existing DBs.
	//
	// The body must match sql/schemas/track_metadata_view.sql.
	if _, err := db.ExecContext(ctx, `DROP VIEW IF EXISTS track_metadata`); err != nil {
		return fmt.Errorf("migration 35: drop view: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		CREATE VIEW track_metadata AS
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
		    COALESCE(rg.original_year, rg.year, r.year, 0) AS year,
		    COALESCE(rg.year, r.year, 0) AS release_year,
		    COALESCE(r.composer, '') AS composer,
		    COALESCE(ft.extension, '') AS file_type,
		    af.sample_rate,
		    af.bit_depth,
		    af.channels,
		    af.bitrate,
		    af.file_size,
		    af.library_id,
		    af.play_count,
		    af.last_played,
		    COALESCE(ca.file_path, '') AS cover_art_path,
		    COALESCE(a.mbid, '') AS artist_mbid,
		    COALESCE(rg.mbid, '') AS release_group_mbid,
		    COALESCE(r.mbid, '') AS recording_mbid
		FROM audio_files af
		LEFT JOIN recordings r ON af.recording_id = r.id
		LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
		LEFT JOIN artist_credit_artist aca ON aca.credit_id = ac.id
		LEFT JOIN artists a ON a.id = aca.artist_id
		LEFT JOIN (
		    SELECT recording_id,
		        MIN(release_group_id) AS release_group_id
		    FROM release_group_recordings
		    GROUP BY recording_id
		) rgr ON r.id = rgr.recording_id
		LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
		LEFT JOIN cover_art ca ON rg.cover_art_id = ca.id
		LEFT JOIN file_types ft ON af.file_type_id = ft.id
	`); err != nil {
		return fmt.Errorf("migration 35: recreate view: %w", err)
	}

	if _, err := db.ExecContext(ctx, "PRAGMA user_version = 35"); err != nil {
		return fmt.Errorf("migration 35: set user_version: %w", err)
	}

	logger.Info("migration 35 complete")

	return nil
}

// migration34FolderBasedGroupKey recomputes every audio_files
// row's group_key with the new folder-based algorithm (album tag
// dropped from the hash inputs).  Tracks in the same parent
// directory + same disc number now share a key regardless of any
// per-track variation in their album tag — fixes the fragmenting
// behaviour where one album would produce N one-track tagging
// groups when its tracks carried slightly different album strings.
//
// After the recompute, tagging_items is wiped and re-aggregated
// from the new keys.  The user's review state is reset; this is a
// blunt instrument but the right one — a partial migration would
// leave fragments of the old shape stranded in pending status.
//
// SAFETY: clears tagging_items unconditionally on existing DBs.
// On fresh DBs (test_data) the tagging_items aggregate at the end
// of the migration just no-ops since audio_files is empty.
func migration34FolderBasedGroupKey(
	ctx context.Context,
	db *sql.DB,
	logger *slog.Logger,
) error {
	logger.Info("applying migration 34: folder-based group_key")

	// Wipe stale state first so the recompute can stream into a
	// clean tagging_items table.
	if _, err := db.ExecContext(ctx, `DELETE FROM tagging_items`); err != nil {
		return fmt.Errorf("migration 34: clear tagging_items: %w", err)
	}

	// Force every audio_files.group_key back to '' so the existing
	// backfill logic (which filters WHERE group_key = '') can
	// recompute every row.
	if _, err := db.ExecContext(ctx,
		`UPDATE audio_files SET group_key = ''`,
	); err != nil {
		return fmt.Errorf("migration 34: clear group_keys: %w", err)
	}

	if err := backfillGroupKeys(ctx, db, logger); err != nil {
		return fmt.Errorf("migration 34: backfill: %w", err)
	}

	if err := aggregateTaggingItems(ctx, db, logger); err != nil {
		return fmt.Errorf("migration 34: aggregate: %w", err)
	}

	if _, err := db.ExecContext(ctx,
		"PRAGMA user_version = 34",
	); err != nil {
		return fmt.Errorf("migration 34: set user_version: %w", err)
	}

	logger.Info("migration 34 complete")

	return nil
}
