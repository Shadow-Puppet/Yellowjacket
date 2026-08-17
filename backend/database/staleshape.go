package database

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"path"
	"slices"
	"strings"

	"yellowjacket/backend/datamap"
)

// This file repairs the one thing `CREATE TABLE IF NOT EXISTS` cannot.
//
// `sql/schemas/` is the single description of the schema and there is no
// migration chain (plan 013): a schema change is one edit to one file.
// That works perfectly for a *new* table, which every install then
// creates, and not at all for a changed one -- `IF NOT EXISTS` reaches
// an existing table only if its shape already matches, and otherwise
// silently no-ops.  The user's answer to that is "delete and rescan"
// (plan 013, open question 1), which is free for everything a rescan
// rebuilds.
//
// It is not free for the catalog.  explore_index is a *downloaded
// artifact*, not something derived from the user's files, and it is the
// largest thing this app stores.  So it went stale instead: plan 014
// added `total_tracks` to the schema and to `indexRowFields` -- the one
// projection every explore read uses -- and no database that already
// existed ever grew the column.  Every Explore search, browse, artist
// page and album page on such an install fails with
// "no such column: total_tracks", while a fresh install is perfectly
// healthy, which is why the tests did not see it.  The same databases
// are stale a second way, from the same plan: their `mbid` columns are
// still TEXT where the schema now declares BLOB, and SQLite does not
// coerce between the two -- a comparison against 16 raw bytes simply
// returns no rows.
//
// The repair is to notice and drop, not to migrate.  A dropped catalog
// costs one artifact download (about a minute); the alternative --
// ALTER TABLE ADD COLUMN, which would handle `total_tracks` alone
// cheaply -- cannot express the TEXT-to-BLOB half at all, and would
// leave those installs quietly broken while reporting success.
//
// Everything except `Authored` is eligible.  `Cache` is rebuildable by
// definition; `Owned` is a projection of the user's files and a rescan
// rebuilds it, which is plan 013's stated answer to exactly this
// situation ("delete and rescan", open question 1); `Derived` is
// computed from Owned.  No `Authored` table is ever dropped here --
// that is the whole point of the datamap, and it is asserted by
// TestAuthoredTablesAreNeverRetired rather than only stated.
//
// What that does *not* buy is immunity for authored rows that reference
// a retired table.  `audio_files` is MIXED KIND: `play_count`,
// `last_played` and `tag_status` are authored columns on an Owned
// table, and they go with it.  Playlists survive as playlists, and
// their entries survive pointing at nothing.  That cost was weighed and
// accepted rather than overlooked -- the alternative is to carry the
// authored columns across the rebuild keyed on file_path, which stays a
// real option if this ever bites harder than it is worth.
//
// **This relies on foreign_keys being ON**, which applyPRAGMAs has
// already done by the time NewDB calls it, and the dependency is not
// cosmetic.  SQLite performs an implicit DELETE before dropping a table
// when foreign keys are enabled, so `playlist_tracks.audio_file_id` --
// declared ON DELETE SET NULL -- is nulled.  With foreign keys off, no
// action fires and those rows keep the ids they had, which a rescan
// then reissues starting from 1: every playlist would silently fill
// with *different songs*.  Nulled entries are merely empty; stale ones
// are wrong, and wrong quietly.  TestRetiringOwnedTablesDoesNotDangle
// is what stops a future reordering turning one into the other.

// retireGroups are tables that must be retired together.  A catalog
// whose rows are gone must not keep the full-text index built over
// them, nor the metadata claiming the import that produced them
// finished -- that marker is exactly what stops the artifact being
// fetched again.  applySchema recreates all three empty immediately
// afterwards, and the ordinary "no index yet" path takes over.
var retireGroups = [][]string{
	{
		"explore_index",
		"explore_index_fts",
		"explore_index_meta",
		"explore_champion_fts",
	},
}

// schemaColumn is one column as the schema file declares it.
type schemaColumn struct {
	name string
	typ  string
}

// retireStaleTables drops every non-authored table whose live shape no
// longer matches what sql/schemas/ declares, plus any table the schema
// no longer describes at all, so applySchema can create the current
// shape afresh.  It runs before applySchema and is a no-op on a new
// database, where the tables do not exist yet.
func retireStaleTables(
	ctx context.Context, db *sql.DB, logger *slog.Logger,
) error {
	declared, err := declaredTables()
	if err != nil {
		return err
	}

	stale := make(map[string]string)

	for table, columns := range declared {
		entry, ok := datamap.Lookup(table)
		if !ok || entry.Kind == datamap.Authored || entry.FTS {
			continue
		}

		// Whether a stale Cache table may be rebuilt is decided per
		// binary, at compile time: the app re-downloads its catalog in
		// about a minute, cmd/indexbuild would re-derive it from ~205 GB
		// of dumps.  See staleshape_policy.go.
		if entry.Kind == datamap.Cache && !retireStaleCache {
			continue
		}

		reason, err := staleReason(ctx, db, table, columns)
		if err != nil {
			return err
		}

		if reason != "" {
			stale[table] = reason
		}
	}

	obsolete, err := obsoleteTables(ctx, db)
	if err != nil {
		return err
	}

	maps.Copy(stale, obsolete)

	if len(stale) == 0 {
		return nil
	}

	return retireGroupsFor(ctx, db, logger, stale)
}

// obsoleteTables are live tables the schema no longer describes at all.
// TestCatalogCoversSchema makes the datamap a complete description of
// the current schema, so a table it does not know is one a past version
// created and this one does not -- plan 013 alone left seven behind
// (recordings, release_groups, artist_credit, artist_credit_artist,
// release_group_recordings, recording_genres) plus the
// schema_migrations table that squashing the chain retired.  They are
// dead weight, and one of them holding a foreign key into a table being
// rebuilt is worse than dead weight.
//
// SQLite's own bookkeeping and FTS shadow tables are not obsolete:
// datamap.Lookup resolves a shadow table to its parent, and IsInternal
// covers the rest.
func obsoleteTables(ctx context.Context, db *sql.DB) (map[string]string, error) {
	rows, err := db.QueryContext(
		ctx, "SELECT name FROM sqlite_master WHERE type = 'table'",
	)
	if err != nil {
		return nil, fmt.Errorf("could not list tables: %w", err)
	}

	defer func() { _ = rows.Close() }()

	out := make(map[string]string)

	for rows.Next() {
		var name string

		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("could not scan table name: %w", err)
		}

		if datamap.IsInternal(name) {
			continue
		}

		if _, known := datamap.Lookup(name); !known {
			out[name] = "the schema no longer describes this table"
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("could not read table list: %w", err)
	}

	return out, nil
}

// retireGroupsFor drops each stale table along with everything its
// retire group says must go with it.
func retireGroupsFor(
	ctx context.Context, db *sql.DB, logger *slog.Logger,
	stale map[string]string,
) error {
	drop := make(map[string]string)

	for table, reason := range stale {
		drop[table] = reason

		for _, group := range retireGroups {
			if !slices.Contains(group, table) {
				continue
			}

			for _, member := range group {
				if _, already := drop[member]; !already {
					drop[member] = "retired with " + table
				}
			}
		}
	}

	return dropDeferred(ctx, db, logger, drop)
}

// dropDeferred drops every named table in one transaction with foreign
// key enforcement deferred to the commit.
//
// The deferral is required and the two obvious alternatives are both
// wrong.  These tables reference each other -- pre-013 `audio_files`
// has a foreign key into `recordings`, which is itself being retired --
// so dropping them one at a time in an arbitrary order fails with
// "FOREIGN KEY constraint failed" on whichever is unlucky enough to go
// first, and there is no order that is safe in general.  Turning
// foreign keys *off* for the duration would fix that and silently take
// the ON DELETE SET NULL on `playlist_tracks.audio_file_id` with it,
// leaving playlist entries pointing at ids a rescan reissues to
// different songs -- the exact failure
// TestRetiringOwnedTablesDoesNotDangle exists to prevent.
//
// Deferring keeps the actions firing while tolerating the inconsistency
// in the middle, and the commit then checks that the end state is
// sound.  It is set inside the transaction because SQLite resets it at
// every commit.
func dropDeferred(
	ctx context.Context, db *sql.DB, logger *slog.Logger,
	drop map[string]string,
) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("could not begin the retire transaction: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, "PRAGMA defer_foreign_keys = ON"); err != nil {
		return fmt.Errorf("could not defer foreign keys: %w", err)
	}

	// Sorted, so a failure is reproducible.  Map order is random, and a
	// bug that depends on which table happens to go first reproduces on
	// one run in three and passes review on the other two -- which is
	// exactly how the foreign-key ordering above reached a real
	// database.  Sorting does not make any order *safe*; the deferral
	// does that.
	for _, table := range slices.Sorted(maps.Keys(drop)) {
		logger.Warn(
			"retiring a table the schema no longer describes",
			"table", table,
			"reason", drop[table],
		)

		if _, err := tx.ExecContext(
			ctx, "DROP TABLE IF EXISTS "+quoteIdent(table),
		); err != nil {
			return fmt.Errorf("could not retire stale table %s: %w", table, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("could not commit the retire: %w", err)
	}

	return nil
}

// staleReason reports why a live table disagrees with its declaration,
// or "" when it agrees.  A column the live table does not have is the
// additive case; a column whose declared type changed is the one an
// ALTER could not fix anyway.  Columns the live table has and the
// schema no longer declares are ignored: they cost nothing and dropping
// the table over one would retire a healthy catalog.
func staleReason(
	ctx context.Context, db *sql.DB, table string, columns []schemaColumn,
) (string, error) {
	live, err := liveColumns(ctx, db, table)
	if err != nil {
		return "", err
	}

	if len(live) == 0 {
		// Not present at all: applySchema is about to create it.
		return "", nil
	}

	for _, col := range columns {
		liveType, present := live[col.name]
		if !present {
			return "missing column " + col.name, nil
		}

		if !sameDeclaredType(col.typ, liveType) {
			return fmt.Sprintf(
				"column %s is %s, schema declares %s",
				col.name, liveType, col.typ,
			), nil
		}
	}

	return "", nil
}

// liveColumns returns the live table's columns and their declared types,
// empty when the table does not exist.
func liveColumns(
	ctx context.Context, db *sql.DB, table string,
) (map[string]string, error) {
	rows, err := db.QueryContext(
		ctx, "SELECT name, type FROM pragma_table_info(?)", table,
	)
	if err != nil {
		return nil, fmt.Errorf("could not inspect table %s: %w", table, err)
	}

	defer func() { _ = rows.Close() }()

	out := make(map[string]string)

	for rows.Next() {
		var name, typ string

		if err := rows.Scan(&name, &typ); err != nil {
			return nil, fmt.Errorf("could not scan column of %s: %w", table, err)
		}

		out[name] = typ
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("could not read columns of %s: %w", table, err)
	}

	return out, nil
}

// sameDeclaredType compares two SQLite type names.  They are compared
// case-insensitively and only on the leading word, so INTEGER matches
// INTEGER and VARCHAR(20) matches VARCHAR -- SQLite's affinity rules
// make finer distinctions meaningless, and a difference that fine is
// not worth retiring a catalog over.  An empty declared type matches
// anything, which is what a column declared with only constraints has.
func sameDeclaredType(declared, live string) bool {
	d := strings.ToUpper(strings.Fields(declared + " ")[0])
	l := strings.ToUpper(strings.Fields(live + " ")[0])

	if d == "" || l == "" {
		return true
	}

	if i := strings.IndexByte(d, '('); i >= 0 {
		d = d[:i]
	}

	if i := strings.IndexByte(l, '('); i >= 0 {
		l = l[:i]
	}

	return d == l
}

// declaredTables parses every CREATE TABLE in sql/schemas/ into its
// column list.  Parsing the schema rather than writing the expectation
// down a second time is the point: a second list is a second thing to
// forget, which is the fault this whole file exists to repair.
func declaredTables() (map[string][]schemaColumn, error) {
	dirEntries, err := schemas.ReadDir("sql/schemas")
	if err != nil {
		return nil, fmt.Errorf("could not read schemas directory: %w", err)
	}

	out := make(map[string][]schemaColumn)

	for _, dirEntry := range dirEntries {
		if dirEntry.IsDir() {
			continue
		}

		content, err := fs.ReadFile(schemas, path.Join("sql/schemas", dirEntry.Name()))
		if err != nil {
			return nil, fmt.Errorf("could not read %s: %w", dirEntry.Name(), err)
		}

		maps.Copy(out, parseCreateTables(string(content)))
	}

	return out, nil
}

// constraintKeywords begin a table constraint rather than a column.
var constraintKeywords = map[string]bool{
	"PRIMARY": true, "FOREIGN": true, "UNIQUE": true,
	"CHECK": true, "CONSTRAINT": true,
}

// parseCreateTables extracts the column names and declared types of
// every non-virtual CREATE TABLE in one schema file.
func parseCreateTables(content string) map[string][]schemaColumn {
	out := make(map[string][]schemaColumn)
	rest := stripLineComments(content)

	for {
		idx := indexFold(rest, "CREATE TABLE ")
		if idx < 0 {
			return out
		}

		rest = rest[idx+len("CREATE TABLE "):]

		head, body, ok := splitTableBody(rest)
		if !ok {
			return out
		}

		if name := tableName(head); name != "" {
			out[name] = parseColumns(body)
		}
	}
}

// tableName pulls the table name out of the text between "CREATE TABLE"
// and its opening parenthesis, dropping an IF NOT EXISTS and any
// quoting.
func tableName(head string) string {
	head = strings.TrimSpace(head)
	head = strings.TrimPrefix(head, "IF NOT EXISTS ")
	head = strings.TrimPrefix(head, "if not exists ")

	fields := strings.Fields(head)
	if len(fields) == 0 {
		return ""
	}

	return strings.Trim(fields[len(fields)-1], `"'`+"`")
}

// splitTableBody returns the text before the table's opening paren and
// the balanced text inside it.
func splitTableBody(s string) (head, body string, ok bool) {
	open := strings.IndexByte(s, '(')
	if open < 0 {
		return "", "", false
	}

	depth := 0

	for i := open; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--

			if depth == 0 {
				return s[:open], s[open+1 : i], true
			}
		}
	}

	return "", "", false
}

// parseColumns splits a table body on its top-level commas and keeps
// the parts that are columns rather than table constraints.
func parseColumns(body string) []schemaColumn {
	var (
		out   []schemaColumn
		depth int
		start int
	)

	parts := make([]string, 0, 8)

	for i := range len(body) {
		switch body[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, body[start:i])
				start = i + 1
			}
		}
	}

	parts = append(parts, body[start:])

	for _, part := range parts {
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}

		// A table constraint need not be followed by a space --
		// "UNIQUE(mbid)" is one field, and reading it as a column name
		// makes an entirely healthy table look stale, which retires a
		// catalog nobody asked to lose.
		head := fields[0]
		if i := strings.IndexByte(head, '('); i >= 0 {
			head = head[:i]
		}

		if constraintKeywords[strings.ToUpper(head)] {
			continue
		}

		col := schemaColumn{name: strings.Trim(head, `"'`+"`")}
		if len(fields) > 1 {
			col.typ = fields[1]
		}

		out = append(out, col)
	}

	return out
}

// stripLineComments removes -- comments, which otherwise contribute
// stray parentheses and commas to the parse.
func stripLineComments(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if idx := strings.Index(line, "--"); idx >= 0 {
			lines[i] = line[:idx]
		}
	}

	return strings.Join(lines, "\n")
}

// indexFold is a case-insensitive strings.Index.
func indexFold(s, substr string) int {
	return strings.Index(strings.ToUpper(s), strings.ToUpper(substr))
}

// quoteIdent quotes a table name for interpolation into DDL, which
// cannot take a bound parameter.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
