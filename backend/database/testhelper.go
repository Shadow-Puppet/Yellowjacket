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

	if err := runMigrations(ctx, db, slog.Default()); err != nil {
		t.Fatalf("could not run migrations: %v", err)
	}

	queries := sqlcgen.New(db)

	t.Cleanup(func() { db.Close() })

	return &DB{
		db:      db,
		Ctx:     ctx,
		Queries: queries,
		logger:  slog.Default(),
	}
}
