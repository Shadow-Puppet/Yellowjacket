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

// NewDB opens the database and creates the schema if it is not there.
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

	if err := applySchema(dbCtx, db); err != nil {
		return nil, err
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

// QueryRowWriter runs a single-row query on the writer connection rather
// than the read pool.
//
// Almost every read should use QueryContext instead.  This exists for
// the one case that cannot: statements referencing a database ATTACHed
// to the writer.  The read pool is a separate sql.DB over the same file,
// so an attachment made on the writer is invisible there and the query
// would fail with "no such table".
func (d *DB) QueryRowWriter(query string, args ...any) *sql.Row {
	return d.db.QueryRowContext(d.Ctx, query, args...)
}

// Logger returns the structured logger bound to this DB. Callers can
// use it to emit timing or diagnostic logs from query-adjacent code.
func (d *DB) Logger() *slog.Logger {
	return d.logger
}

// exploreIndexFTSTriggers is the sole definition of the explore_index →
// FTS5 sync triggers.
//
// They live here rather than in sql/schemas/explore_index.sql because a
// bulk load drops and recreates them (see SuspendExploreIndexFTS), so
// the runtime needs them as statements either way.  Defining them in
// both places would be two copies free to drift.
var exploreIndexFTSTriggers = []string{
	`CREATE TRIGGER explore_index_ai AFTER INSERT ON explore_index BEGIN
		INSERT INTO explore_index_fts(rowid, title, artist_name, aliases)
		VALUES (new.id, new.title, new.artist_name, new.aliases);
	END`,
	`CREATE TRIGGER explore_index_ad AFTER DELETE ON explore_index BEGIN
		INSERT INTO explore_index_fts(explore_index_fts, rowid, title, artist_name, aliases)
		VALUES ('delete', old.id, old.title, old.artist_name, old.aliases);
	END`,
	// Narrowed to the three columns the FTS table actually indexes, and
	// guarded on them having changed.  An UPDATE that leaves all three
	// alone has nothing to re-index, and re-indexing it is not free: an
	// FTS5 delete has to find the old row's postings in a multi-million
	// row index, which is the ~31 rows/s figure below.
	//
	// This is not a micro-optimisation.  Every writer here upserts, and
	// the merge rules keep existing values (`CASE WHEN excluded.title
	// != '' ...`), so the common write is a row arriving unchanged: the
	// discography backfill re-browsing a known artist, the incremental
	// dump refreshing popularity.  Each of those used to pay a full
	// delete + insert against the FTS index while holding the single
	// writer connection — measured at 91% of the app's CPU, with the
	// play path queued behind it.
	`CREATE TRIGGER explore_index_au AFTER UPDATE OF title, artist_name, aliases
	ON explore_index
	WHEN old.title IS NOT new.title
		OR old.artist_name IS NOT new.artist_name
		OR old.aliases IS NOT new.aliases
	BEGIN
		INSERT INTO explore_index_fts(explore_index_fts, rowid, title, artist_name, aliases)
		VALUES ('delete', old.id, old.title, old.artist_name, old.aliases);
		INSERT INTO explore_index_fts(rowid, title, artist_name, aliases)
		VALUES (new.id, new.title, new.artist_name, new.aliases);
	END`,
}

// createExploreIndexFTSTriggers installs the sync triggers.  Safe to
// call on a database that already has them.
//
// It drops first rather than tolerating "already exists", because a
// trigger is a definition and not a row: an install that already has
// the old one would otherwise keep it forever, and these definitions
// are exactly where this table's write cost is decided.  Three DDL
// statements against a table with no rows to rewrite, on open.
func createExploreIndexFTSTriggers(ctx context.Context, db *sql.DB) error {
	if err := dropExploreIndexFTSTriggers(ctx, db); err != nil {
		return err
	}

	for _, stmt := range exploreIndexFTSTriggers {
		if _, err := db.ExecContext(ctx, stmt); err != nil &&
			!strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("create explore FTS trigger: %w", err)
		}
	}

	return nil
}

// exploreIndexFTSTriggerNames is what both the drop paths remove.
var exploreIndexFTSTriggerNames = []string{
	"explore_index_ai", "explore_index_ad", "explore_index_au",
}

func dropExploreIndexFTSTriggers(ctx context.Context, db *sql.DB) error {
	for _, name := range exploreIndexFTSTriggerNames {
		if _, err := db.ExecContext(
			ctx, "DROP TRIGGER IF EXISTS "+name,
		); err != nil {
			return fmt.Errorf("drop explore FTS trigger %s: %w", name, err)
		}
	}

	return nil
}

// SuspendExploreIndexFTS drops the FTS sync triggers so a bulk load can
// write explore_index without paying per-row FTS maintenance.
//
// Row-at-a-time FTS upkeep is what makes a full dump import take
// nearly a day: a bulk DELETE fires the delete trigger once per row,
// which buries the FTS5 index in delete markers and stale segments, and
// every subsequent upsert then works against that debris.  Measured on
// a real import, assembly runs at ~31 rows/s with the triggers attached
// and ~4,700 rows/s without.
//
// Callers MUST pair this with ResumeExploreIndexFTS — while suspended,
// explore_index_fts stops tracking the table and search goes stale.
func (d *DB) SuspendExploreIndexFTS() error {
	if err := dropExploreIndexFTSTriggers(d.Ctx, d.db); err != nil {
		return fmt.Errorf("suspend explore FTS: %w", err)
	}

	return nil
}

// ResumeExploreIndexFTS reinstates the sync triggers and rebuilds the
// FTS index from the content table, discarding whatever accumulated
// while it was suspended.  The rebuild is a single linear pass and is
// far cheaper than the per-row maintenance it replaces.
//
// Safe to call when the triggers are already present, so it can run
// from a defer on both the success and failure paths.
func (d *DB) ResumeExploreIndexFTS() error {
	if err := createExploreIndexFTSTriggers(d.Ctx, d.db); err != nil {
		return fmt.Errorf("resume explore FTS: %w", err)
	}

	if _, err := d.db.ExecContext(
		d.Ctx, "INSERT INTO explore_index_fts(explore_index_fts) VALUES('rebuild')",
	); err != nil {
		return fmt.Errorf("resume explore FTS: rebuild: %w", err)
	}

	return nil
}

// applySchema creates the full schema.
//
// The schema files under sql/schemas are CREATE ... IF NOT EXISTS and
// declare the current, latest shape of every table — so running them
// against a fresh database produces exactly that shape, and running
// them against a database already at that shape does nothing.  That is
// the whole mechanism; there is no migration chain and no
// schema_migrations table.
//
// There was one, and it was squashed (see .planning/plans/013): a chain
// only earns its keep once real user databases exist in the wild, and
// until then it is a second description of the schema that can drift
// from the first — which this project has already been bitten by once.
func applySchema(ctx context.Context, db *sql.DB) error {
	dirEntries, err := schemas.ReadDir("sql/schemas")
	if err != nil {
		return fmt.Errorf("could not read schemas directory: %w", err)
	}

	for _, dirEntry := range dirEntries {
		if dirEntry.IsDir() {
			continue
		}

		filePath := path.Join("sql/schemas", dirEntry.Name())

		sqlContent, err := fs.ReadFile(schemas, filePath)
		if err != nil {
			return fmt.Errorf("could not read file %s: %w", filePath, err)
		}

		if _, err := db.ExecContext(ctx, string(sqlContent)); err != nil {
			return fmt.Errorf("error executing sql from file %s: %w", filePath, err)
		}
	}

	// The FTS sync triggers are defined in Go, not in the schema files,
	// because the bulk-load path drops and recreates them.
	if err := createExploreIndexFTSTriggers(ctx, db); err != nil {
		return fmt.Errorf("could not create explore FTS triggers: %w", err)
	}

	return nil
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
