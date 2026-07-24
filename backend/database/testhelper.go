package database

import (
	"database/sql"
	"io/fs"
	"log/slog"
	"path"
	"testing"

	_ "modernc.org/sqlite" // Register sqlite driver.

	"yellowjacket/backend/database/sql/sqlcgen"
)

// NewTestDB returns an in-memory SQLite database that mirrors the
// production setup (PRAGMAs + all migrations). The database is
// automatically closed when the test completes via t.Cleanup.
func NewTestDB(t *testing.T) *DB {
	t.Helper()

	db, err := sql.Open(
		"sqlite",
		":memory:?_busy_timeout=5000&_journal_mode=WAL",
	)
	if err != nil {
		t.Fatalf("could not open test database: %v", err)
	}

	db.SetMaxOpenConns(1)

	ctx := t.Context()

	if err := applyPRAGMAs(ctx, db); err != nil {
		t.Fatalf("could not apply PRAGMAs: %v", err)
	}

	dirEntries, err := schemas.ReadDir("sql/schemas")
	if err != nil {
		t.Fatalf("could not read schemas directory: %v", err)
	}

	for _, dirEntry := range dirEntries {
		if !dirEntry.IsDir() {
			filePath := path.Join("sql/schemas", dirEntry.Name())

			sqlContent, err := fs.ReadFile(schemas, filePath)
			if err != nil {
				t.Fatalf("could not read file %s: %v", filePath, err)
			}

			if _, err = db.ExecContext(ctx, string(sqlContent)); err != nil {
				t.Fatalf(
					"error executing sql from file %s: %v",
					filePath, err,
				)
			}
		}
	}

	if err := runMigrations(ctx, db, slog.Default(), ":memory:"); err != nil {
		t.Fatalf("could not run migrations: %v", err)
	}

	// Insert a sentinel library row at id=0 so audio_files inserts
	// using the DEFAULT library_id=0 satisfy the FK constraint.
	if _, err := db.ExecContext(
		ctx,
		"INSERT INTO libraries (id, name, path) VALUES (0, 'Test', '/test')",
	); err != nil {
		t.Fatalf("could not insert test library: %v", err)
	}

	queries := sqlcgen.New(db)

	t.Cleanup(func() { _ = db.Close() })

	return &DB{
		db:  db,
		Ctx: ctx,
		// The in-memory test DB shares one connection, so reads and
		// writes use the same handle; ReadQueries aliases Queries.
		Queries:     queries,
		ReadQueries: queries,
		logger:      slog.Default(),
	}
}

// NewTestDBWithLibrary returns a test DB with a library row
// pre-inserted. Returns the DB and the library ID.
func NewTestDBWithLibrary(
	t *testing.T,
	name, libPath string,
) (*DB, int64) {
	t.Helper()

	db := NewTestDB(t)

	lib, err := db.Queries.CreateLibrary(
		db.Ctx,
		sqlcgen.CreateLibraryParams{
			Name: name,
			Path: libPath,
		},
	)
	if err != nil {
		t.Fatalf("could not create test library: %v", err)
	}

	return db, lib.ID
}
