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

	// Enable foreign key enforcement — SQLite disables it by
	// default, which means ON DELETE CASCADE will not work without
	// this pragma.
	if _, err := db.ExecContext(
		dbCtx, "PRAGMA foreign_keys = ON",
	); err != nil {
		return nil, fmt.Errorf(
			"could not enable foreign keys: %w", err,
		)
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
