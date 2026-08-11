//go:build dev

package testctl

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// snapshotDir keeps snapshots inside the sandbox's own YJ_HOME, so
// deleting the home deletes them and nothing leaks between runs.
func snapshotDir() (string, error) {
	dir := filepath.Join(filepath.Dir(dbPath()), "testctl")

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	return dir, nil
}

func snapshotPath(name string) (string, error) {
	if !safeName.MatchString(name) {
		return "", errBadName
	}

	dir, err := snapshotDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, name+".db"), nil
}

// handleSnapshot copies the live database with VACUUM INTO, which takes
// a consistent copy without stopping the app or closing the handle.
//
//	POST /__test/db/snapshot?name=pristine
func handleSnapshot(d Deps, r *http.Request) (any, error) {
	path, err := snapshotPath(r.URL.Query().Get("name"))
	if err != nil {
		return nil, err
	}

	// VACUUM INTO refuses to overwrite, and a spec re-snapshotting the
	// same name means "replace", not "fail".
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	if _, err := d.DB.ExecContext("VACUUM INTO ?", path); err != nil {
		return nil, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	return map[string]any{"path": path, "bytes": info.Size()}, nil
}

// handleRestore puts the database back to a previous snapshot without
// restarting the app.
//
// It copies rows rather than files because the app holds the file open
// (two connection pools, WAL) and cannot be made to reopen it from
// here.  ATTACH runs on the writer connection — an attachment is
// invisible to the read pool, which is a separate sql.DB over the same
// file, so anything touching `snap.` must avoid QueryContext.
//
//	POST /__test/db/restore?name=pristine
func handleRestore(d Deps, r *http.Request) (any, error) {
	path, err := snapshotPath(r.URL.Query().Get("name"))
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(path); err != nil {
		return nil, errNoSnapshot
	}

	if _, err := d.DB.ExecContext("ATTACH DATABASE ? AS snap", path); err != nil {
		return nil, err
	}

	defer func() {
		if _, err := d.DB.ExecContext("DETACH DATABASE snap"); err != nil {
			d.Logger.Error("testctl could not detach snapshot",
				"err", err.Error())
		}
	}()

	tables, err := restorableTables(d)
	if err != nil {
		return nil, err
	}

	if err := copyTables(d, tables); err != nil {
		return nil, err
	}

	if err := checkForeignKeys(d); err != nil {
		return nil, err
	}

	// search_index and lyrics_index are FTS5 tables maintained by Go,
	// not by triggers, so a row copy leaves them stale.  The explore
	// FTS tables *are* trigger-maintained off explore_index and
	// re-synced by the copy above.
	if err := d.DB.RebuildSearchIndex(); err != nil {
		return nil, err
	}

	if err := d.DB.RebuildLyricsIndex(); err != nil {
		return nil, err
	}

	return map[string]any{"restored": path, "tables": len(tables)}, nil
}

// restorableTables lists the ordinary tables to copy.
//
// Two kinds are excluded.  FTS5 virtual tables cannot be written by
// SELECT * (their column shape is not their storage shape), and every
// shadow table backing one — <name>_data, _idx, _content, _docsize,
// _config — is an implementation detail that must be rebuilt rather
// than copied.
func restorableTables(d Deps) ([]string, error) {
	// main.sqlite_master is readable from the read pool; only `snap.`
	// requires the writer connection.
	rows, err := d.DB.QueryContext(
		`SELECT name, COALESCE(sql, '') FROM main.sqlite_master
		 WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		 ORDER BY name`,
	)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	var (
		ordinary []string
		virtual  []string
	)

	for rows.Next() {
		var name, ddl string
		if err := rows.Scan(&name, &ddl); err != nil {
			return nil, err
		}

		if strings.HasPrefix(strings.ToUpper(ddl), "CREATE VIRTUAL TABLE") {
			virtual = append(virtual, name)

			continue
		}

		ordinary = append(ordinary, name)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]string, 0, len(ordinary))

	for _, name := range ordinary {
		if isShadowTable(name, virtual) {
			continue
		}

		out = append(out, name)
	}

	return out, nil
}

// isShadowTable reports whether name is storage belonging to one of the
// given virtual tables.
func isShadowTable(name string, virtual []string) bool {
	for _, v := range virtual {
		if strings.HasPrefix(name, v+"_") {
			return true
		}
	}

	return false
}

// copyTables replaces the contents of every named table from `snap`.
//
// Foreign keys are switched **off** for the duration, not merely
// deferred.  Deferring only postpones the *check*; it does not stop
// ON DELETE CASCADE from firing, and the tables are copied in name
// order, which is not dependency order — so `DELETE FROM libraries`
// cascades away the rows of a child table that was restored earlier in
// the loop, and the commit then fails with a bare "FOREIGN KEY
// constraint failed (787)" that points at nothing.  Measured, not
// theorised.
//
// PRAGMA foreign_keys is a no-op inside a transaction, so it has to be
// set on the connection around it.  That is safe here only because the
// writer is a single connection and this is a dev-only endpoint; the
// caller re-enables and then verifies with PRAGMA foreign_key_check,
// so an inconsistent restore is reported rather than left in place.
func copyTables(d Deps, tables []string) error {
	if _, err := d.DB.ExecContext("PRAGMA foreign_keys = OFF"); err != nil {
		return err
	}

	defer func() {
		if _, err := d.DB.ExecContext("PRAGMA foreign_keys = ON"); err != nil {
			d.Logger.Error("testctl could not re-enable foreign keys",
				"err", err.Error())
		}
	}()

	tx, err := d.DB.BeginTx()
	if err != nil {
		return err
	}

	defer func() { _ = tx.Rollback() }()

	for _, name := range tables {
		quoted := `"` + strings.ReplaceAll(name, `"`, `""`) + `"`

		if _, err := tx.Exec("DELETE FROM main." + quoted); err != nil {
			return err
		}

		if _, err := tx.Exec(
			"INSERT INTO main." + quoted + " SELECT * FROM snap." + quoted,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// checkForeignKeys verifies the restored database is self-consistent,
// since the copy ran with enforcement off.
func checkForeignKeys(d Deps) error {
	rows, err := d.DB.QueryContext("PRAGMA main.foreign_key_check")
	if err != nil {
		return err
	}

	defer func() { _ = rows.Close() }()

	var tables []string

	for rows.Next() {
		var (
			table, parent string
			rowid, fkid   sql.NullInt64
		)

		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			return err
		}

		tables = append(tables, table+"->"+parent)
	}

	if err := rows.Err(); err != nil {
		return err
	}

	if len(tables) > 0 {
		return fmt.Errorf("%w: %s", errInconsistent,
			strings.Join(tables[:min(len(tables), 5)], ", "))
	}

	return nil
}

// scanAll turns a result set into JSON-shaped rows.  Values arrive as
// any so that a spec can assert on them without the endpoint having to
// know the schema.
func scanAll(rows *sql.Rows) (any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	out := []map[string]any{}

	for rows.Next() {
		cells := make([]any, len(cols))
		ptrs := make([]any, len(cols))

		for i := range cells {
			ptrs[i] = &cells[i]
		}

		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}

		row := make(map[string]any, len(cols))

		for i, col := range cols {
			// []byte encodes as base64 in JSON, which is unreadable
			// for the text columns this mostly returns.
			if b, ok := cells[i].([]byte); ok {
				row[col] = string(b)

				continue
			}

			row[col] = cells[i]
		}

		out = append(out, row)
	}

	return map[string]any{"columns": cols, "rows": out}, rows.Err()
}
