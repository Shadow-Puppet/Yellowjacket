package datamap_test

import (
	"fmt"
	"testing"

	"yellowjacket/backend/database"
	"yellowjacket/backend/datamap"
)

// liveTables returns every table and view in a freshly migrated schema.
func liveTables(t *testing.T, db *database.DB) []string {
	t.Helper()

	rows, err := db.QueryContext(
		`SELECT name FROM sqlite_master
		 WHERE type IN ('table', 'view')
		 ORDER BY name`,
	)
	if err != nil {
		t.Fatalf("read sqlite_master: %v", err)
	}

	defer func() { _ = rows.Close() }()

	var names []string

	for rows.Next() {
		var name string

		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}

		names = append(names, name)
	}

	return names
}

// Every table in the schema must be claimed by exactly one catalog
// entry.  This is the mechanism that stops a new table from silently
// having no deletion policy — the failure mode that made libraries
// unremovable when tagging_items was added.
func TestCatalogCoversSchema(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)

	for _, name := range liveTables(t, db) {
		if datamap.IsInternal(name) {
			continue
		}

		if _, ok := datamap.Lookup(name); !ok {
			t.Errorf(
				"table %q exists in the schema but is not in the datamap "+
					"catalog — add an entry stating its Kind and Lifetime",
				name,
			)
		}
	}
}

// The reverse direction: a catalog entry naming a table that no longer
// exists means the catalog has drifted.
func TestCatalogHasNoStaleEntries(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)

	live := make(map[string]bool)
	for _, name := range liveTables(t, db) {
		live[name] = true
	}

	for _, entry := range datamap.Tables() {
		if !live[entry.Name] {
			t.Errorf(
				"catalog lists %q but it is not in the schema",
				entry.Name,
			)
		}
	}
}

type foreignKey struct {
	child    string
	from     string
	parent   string
	onDelete string
}

// liveForeignKeys reads every foreign key in the schema.
func liveForeignKeys(t *testing.T, db *database.DB) []foreignKey {
	t.Helper()

	var out []foreignKey

	for _, table := range liveTables(t, db) {
		if datamap.IsInternal(table) {
			continue
		}

		rows, err := db.QueryContext(
			fmt.Sprintf("PRAGMA foreign_key_list(%q)", table),
		)
		if err != nil {
			continue // views have none
		}

		for rows.Next() {
			var (
				id, seq                                 int
				parent, from, to, onUpd, onDel, matchOn string
			)

			if err := rows.Scan(
				&id, &seq, &parent, &from, &to, &onUpd, &onDel, &matchOn,
			); err != nil {
				continue
			}

			out = append(out, foreignKey{
				child:    table,
				from:     from,
				parent:   parent,
				onDelete: onDel,
			})
		}

		_ = rows.Close()
	}

	return out
}

// A foreign key with NO ACTION blocks its parent's deletion until
// application code clears the child rows.  Any table with such a key
// must therefore declare Lifetime "swept" — an assertion that some
// removal path or janitor actually deletes them.  Declaring "retained"
// or "cascade" while holding a NO ACTION key is the exact shape of the
// tagging_items bug.
func TestNoActionForeignKeysAreDeclaredSwept(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)

	for _, fk := range liveForeignKeys(t, db) {
		if fk.onDelete != "NO ACTION" {
			continue
		}

		entry, ok := datamap.Lookup(fk.child)
		if !ok {
			continue // TestCatalogCoversSchema reports this
		}

		if entry.Lifetime != datamap.Swept {
			t.Errorf(
				"%s.%s references %s with ON DELETE NO ACTION, so it "+
					"blocks deletion of %s — but the catalog declares "+
					"Lifetime %q. Either declare it %q and delete the rows "+
					"explicitly, or give the key an ON DELETE action.",
				fk.child, fk.from, fk.parent, fk.parent,
				entry.Lifetime, datamap.Swept,
			)
		}
	}
}

// Declared cascade/set-null lifetimes must match the actual schema, so
// the catalog cannot quietly drift from what SQLite enforces.
func TestLifetimesMatchSchema(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)

	actual := make(map[string]map[string]bool)

	for _, fk := range liveForeignKeys(t, db) {
		if actual[fk.child] == nil {
			actual[fk.child] = make(map[string]bool)
		}

		actual[fk.child][fk.onDelete] = true
	}

	for _, entry := range datamap.Tables() {
		switch entry.Lifetime {
		case datamap.Cascade:
			if !actual[entry.Name]["CASCADE"] {
				t.Errorf(
					"%s declares Lifetime cascade but has no "+
						"ON DELETE CASCADE foreign key",
					entry.Name,
				)
			}
		case datamap.SetNull:
			if !actual[entry.Name]["SET NULL"] {
				t.Errorf(
					"%s declares Lifetime set-null but has no "+
						"ON DELETE SET NULL foreign key",
					entry.Name,
				)
			}
		case datamap.Swept, datamap.Retained:
			// No schema-level obligation.
		}
	}
}

// Authored data is unrecoverable, so it must never be removed as a side
// effect of deleting owned data.  Cascade is allowed only where the
// catalog explains why (play_history, queue_tracks); this test pins the
// set so a new cascade onto authored data is a deliberate decision.
func TestAuthoredCascadesAreDeliberate(t *testing.T) {
	t.Parallel()

	allowed := map[string]bool{
		"play_history": true,
		"queue_tracks": true,

		// Download history is scoped to the library it imported into.
		// When that library is removed the files it acquired go with
		// it, so a download describing "fetch this into library 3" has
		// nothing left to mean. Keeping the rows would leave history
		// pointing at a library the user deleted.
		"download_downloads": true,

		// Items belong to their download and have no independent
		// meaning; they cascade with it.
		"download_items": true,

		// A request says "put this in library 3". Delete that library
		// and there is no longer anywhere for it to go, so the request
		// has nothing left to mean — the same reasoning as its
		// downloads. The second cascade, artist request to derived
		// album requests, is the point of the subscription:
		// unsubscribing from an artist must stop the albums it queued
		// on the user's behalf.
		"download_requests": true,

		// An exclusion says "do not import this path into library 3".
		// Remove that library and no scan will ever visit the path
		// again, so the row has nothing left to exclude it from — and
		// the data it protects is the *absence* of a row, which the
		// library removal has already achieved for everything.  Adding
		// the library back is the user asking to import it afresh.
		"excluded_paths": true,
	}

	for _, entry := range datamap.ByKind(datamap.Authored) {
		if entry.Lifetime == datamap.Cascade && !allowed[entry.Name] {
			t.Errorf(
				"authored table %q cascades on delete — authored data is "+
					"unrecoverable, so this needs an explicit exemption "+
					"and a note explaining it",
				entry.Name,
			)
		}
	}
}

// Every catalogued table needs a note; the classification is only useful
// if the reasoning is written down.
func TestEveryEntryHasANote(t *testing.T) {
	t.Parallel()

	for _, entry := range datamap.Tables() {
		if entry.Note == "" {
			t.Errorf("catalog entry %q has no Note", entry.Name)
		}
	}

	for _, dir := range datamap.Directories() {
		if dir.Note == "" {
			t.Errorf("catalog directory %q has no Note", dir.Name)
		}
	}
}

// FTS shadow tables must resolve to their parent entry rather than
// needing catalogue entries of their own.
func TestFTSShadowResolution(t *testing.T) {
	t.Parallel()

	entry, ok := datamap.Lookup("search_index_data")
	if !ok {
		t.Fatal("search_index_data did not resolve to a catalog entry")
	}

	if entry.Name != "search_index" {
		t.Errorf("resolved to %q, want search_index", entry.Name)
	}
}
