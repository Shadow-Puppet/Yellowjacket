//go:build indexbuild

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"path"
	"strings"

	_ "modernc.org/sqlite"

	"yellowjacket/backend/datamap"
	"yellowjacket/backend/system"
)

// retireLibraryTables drops every table in the index database that is
// not part of the catalog, before the schema is applied over it.
//
// This database is not an install.  Nothing scans a library into it,
// nothing plays a track, nothing authors a playlist: every table the
// datamap does not classify as Cache is empty by construction, and so
// is anything left over from a shape the schema no longer describes.
// The catalog is the opposite — it is the ~205 GB of dumps this job
// exists to avoid re-downloading, so it is never touched here.
//
// The alternative was to give this database a migration chain that the
// app deliberately does not have.  `sql/schemas/` is CREATE ... IF NOT
// EXISTS, which reaches an *existing* table only if its shape already
// matches; plan 013 reshaped audio_files and every launch since has
// failed on "no such column: album_id" while applying an index to the
// old table.  A user's answer to that is "delete and rescan" (plan 013,
// open question 1).  This is that answer, for the one database where
// deleting the library half costs nothing and deleting the other half
// costs a day.
func retireLibraryTables(ctx context.Context, logger *slog.Logger) error {
	dataDir, err := system.GetUserDataDirPath()
	if err != nil {
		return fmt.Errorf("resolve data dir: %w", err)
	}

	db, err := sql.Open("sqlite", path.Join(dataDir, "yj.db"))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	defer func() { _ = db.Close() }()

	// Virtual tables first: dropping one takes its four shadow tables
	// with it, so a second pass sees a schema with nothing dangling.
	for _, virtual := range []bool{true, false} {
		dropped, err := dropDisposable(ctx, db, virtual)
		if err != nil {
			return err
		}

		if len(dropped) > 0 {
			logger.Info(
				"retired tables the catalog does not need",
				"virtual", virtual,
				"tables", dropped,
			)
		}
	}

	return nil
}

// dropDisposable drops one pass of non-catalog objects and returns what
// it dropped.  When virtual is true it considers only FTS5 virtual
// tables; otherwise it takes the ordinary tables and views left after
// that pass.
func dropDisposable(
	ctx context.Context,
	db *sql.DB,
	virtual bool,
) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT name, type, COALESCE(sql, '')
		FROM sqlite_master
		WHERE type IN ('table', 'view')
	`)
	if err != nil {
		return nil, fmt.Errorf("read schema: %w", err)
	}

	type object struct{ name, kind string }

	var doomed []object

	for rows.Next() {
		var obj object

		var ddl string

		if err := rows.Scan(&obj.name, &obj.kind, &ddl); err != nil {
			_ = rows.Close()

			return nil, fmt.Errorf("read schema: %w", err)
		}

		isVirtual := strings.HasPrefix(ddl, "CREATE VIRTUAL")
		if isVirtual != virtual || keepTable(obj.name) {
			continue
		}

		doomed = append(doomed, obj)
	}

	_ = rows.Close()

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read schema: %w", err)
	}

	names := make([]string, 0, len(doomed))

	for _, obj := range doomed {
		stmt := `DROP TABLE IF EXISTS "` + obj.name + `"`
		if obj.kind == "view" {
			stmt = `DROP VIEW IF EXISTS "` + obj.name + `"`
		}

		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return nil, fmt.Errorf("drop %s: %w", obj.name, err)
		}

		names = append(names, obj.name)
	}

	return names, nil
}

// keepTable reports whether an object survives.  SQLite's own
// bookkeeping is not ours to drop, and Cache is the catalog and its
// neighbours — expensive to rebuild, which is the whole point of the
// persistent volume this runs against.  Everything else goes, including
// tables the datamap has never heard of: an uncatalogued table in this
// database is one the schema stopped describing.
func keepTable(name string) bool {
	if datamap.IsInternal(name) {
		return true
	}

	entry, known := datamap.Lookup(name)

	return known && entry.Kind == datamap.Cache
}
