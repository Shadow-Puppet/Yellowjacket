package library

import (
	"testing"

	"yellowjacket/backend/datamap"
)

// staleTolerated lists tables that deliberately keep rows after the data
// they describe is gone.  Each needs a reason: the point of this list is
// that tolerating a leak becomes a decision somebody wrote down, not an
// oversight nobody noticed.
var staleTolerated = map[string]string{
	"file_types": "static lookup rows seeded from code, not user data",
	"search_index": "contentless FTS5 cannot delete individual rows; " +
		"stale entries are filtered by joining track_metadata and are " +
		"cleared by a full rescan",
	"lyrics_index": "contentless FTS5, same constraint as search_index",
	"schema_migrations": "global migration bookkeeping, not scoped to any " +
		"library; removing the only library must not touch it",
}

// Removing the only library must leave no owned or derived rows behind.
//
// The table list comes from the datamap catalog rather than being
// hardcoded, so a newly added table is covered by this test the moment it
// is catalogued — which is the mechanism that would have caught
// tagging_items blocking removal, and the cover art variants leaking.
func TestRemoveLibraryLeavesNoOwnedOrDerivedRows(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)

	library := seedRemovableLibrary(t, lib, "/nonexistent/cover.jpg")

	if _, err := lib.RemoveLibrary(library.ID); err != nil {
		t.Fatalf("RemoveLibrary: %v", err)
	}

	for _, entry := range datamap.Tables() {
		if entry.Kind != datamap.Owned && entry.Kind != datamap.Derived {
			continue
		}

		// FTS5 virtual tables do not answer COUNT(*) meaningfully.
		if entry.FTS {
			continue
		}

		if reason, exempt := staleTolerated[entry.Name]; exempt {
			t.Logf("skipping %s: %s", entry.Name, reason)

			continue
		}

		if n := countRows(t, lib, entry.Name); n != 0 {
			t.Errorf(
				"%s (%s) has %d rows after the only library was removed. "+
					"Either delete them in RemoveLibrary, or add an entry "+
					"to staleTolerated explaining why they stay.",
				entry.Name, entry.Kind, n,
			)
		}
	}
}

// Authored data must survive removal of the library it was created
// against — losing it is unrecoverable, so it must never be a casualty
// of cleaning up owned data.
func TestRemoveLibraryPreservesAuthoredData(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)

	library := seedRemovableLibrary(t, lib, "/nonexistent/cover.jpg")

	if _, err := lib.db.ExecContext(
		`INSERT INTO playlists (name) VALUES ('Keep Me')`,
	); err != nil {
		t.Fatalf("seed playlist: %v", err)
	}

	if _, err := lib.RemoveLibrary(library.ID); err != nil {
		t.Fatalf("RemoveLibrary: %v", err)
	}

	if n := countRows(t, lib, "playlists"); n != 1 {
		t.Errorf("playlists = %d rows after removal, want 1 preserved", n)
	}
}

// Every table the catalog marks as needing an explicit sweep must
// actually reach zero, or be listed as tolerated.  This is a narrower
// restatement of the leak test aimed at the Lifetime axis rather than
// the Kind axis, so a table declared "swept" that nothing sweeps is
// caught.
func TestSweptTablesAreActuallySwept(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)

	library := seedRemovableLibrary(t, lib, "/nonexistent/cover.jpg")

	if _, err := lib.RemoveLibrary(library.ID); err != nil {
		t.Fatalf("RemoveLibrary: %v", err)
	}

	for _, entry := range datamap.Tables() {
		if entry.Lifetime != datamap.Swept || entry.FTS {
			continue
		}

		// Cache tables are swept by the janitor on their own schedule,
		// not by library removal.
		if entry.Kind == datamap.Cache {
			continue
		}

		if _, exempt := staleTolerated[entry.Name]; exempt {
			continue
		}

		if n := countRows(t, lib, entry.Name); n != 0 {
			t.Errorf(
				"%s declares Lifetime swept but still has %d rows after "+
					"removal — nothing is sweeping it",
				entry.Name, n,
			)
		}
	}
}
