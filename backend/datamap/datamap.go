// Package datamap is the catalog of everything YellowJacket persists.
//
// It exists because deletion logic used to be written per call site and
// lived far from the thing being deleted.  Nobody adding a table could
// know the full set of places that needed updating, so tables leaked
// (rows nothing ever removed), files leaked (thumbnails whose database
// row was gone), and in one case a new table's foreign key silently made
// libraries unremovable.
//
// Every table, view, and on-disk asset directory is classified along two
// axes that between them determine every policy worth having:
//
//   - Kind — what it costs to lose the data.
//   - Lifetime — how rows or files are removed.
//
// The catalog is plain data with no dependency on any service, so tests
// can assert it against a live schema.  The rule that gives it teeth:
// every table in the database must be claimed by exactly one entry here,
// enforced by TestCatalogCoversSchema.  A new table fails the build until
// somebody states what it is and how it dies.
package datamap

import "strings"

// Kind classifies persisted data by what losing it costs.
type Kind string

const (
	// Owned data is a projection of the user's audio files.  The files
	// on disk are the source of truth; a rescan rebuilds all of it.
	Owned Kind = "owned"

	// Authored data was created by the user and exists nowhere else.
	// Losing it is unrecoverable data loss — it must never be deleted
	// as a side effect of anything.
	Authored Kind = "authored"

	// Derived data is computed from owned data and is cheap to rebuild.
	// It may be deleted freely and must never block deletion of the
	// data it was derived from.
	Derived Kind = "derived"

	// Cache data came from the network or a MusicBrainz dump.  It is
	// rebuildable but expensive — rate limits, or a multi-hour index
	// build — so it is evicted on its own schedule rather than being
	// tied to the lifetime of anything else.
	Cache Kind = "cache"
)

// Lifetime says how rows leave a table.  It is declared here and checked
// against the live schema by TestLifetimesMatchSchema, so a foreign key
// that disagrees with the stated policy fails the tests.
type Lifetime string

const (
	// Cascade means a foreign key with ON DELETE CASCADE removes rows
	// when the parent goes.  No application code required.
	Cascade Lifetime = "cascade"

	// SetNull means a foreign key with ON DELETE SET NULL orphans the
	// row deliberately, keeping it alive without its parent.
	SetNull Lifetime = "set-null"

	// Swept means application code must delete these rows explicitly —
	// either in a removal path or in the maintenance janitor.  Any table
	// with a NO ACTION foreign key must declare this, because such a key
	// blocks its parent's deletion until someone clears the child rows.
	Swept Lifetime = "swept"

	// Retained means rows are never removed automatically.  Only an
	// explicit user action deletes them.
	Retained Lifetime = "retained"
)

// Table is one catalog entry.
type Table struct {
	// Name is the SQL table or view name.
	Name string
	// Kind is what the data costs to lose.
	Kind Kind
	// Lifetime is how rows are removed.
	Lifetime Lifetime
	// FTS marks an FTS5 virtual table, which SQLite backs with four
	// shadow tables (_config, _data, _docsize, _idx).  Those are
	// implementation detail and are resolved to this entry.
	FTS bool
	// Note records why the classification is what it is, particularly
	// where a table is not purely one kind.
	Note string
}

// ftsShadowSuffixes are the tables SQLite creates behind an FTS5 virtual
// table.  They belong to their parent and are never catalogued directly.
var ftsShadowSuffixes = []string{
	"_config", "_data", "_docsize", "_idx",
}

// internalTables are SQLite's own bookkeeping, outside our control.
var internalTables = map[string]bool{
	"sqlite_sequence": true,
	"sqlite_stat1":    true,
	"sqlite_stat4":    true,
}

// tables is the catalog.  Keep it alphabetical.
var tables = []Table{
	{
		Name: "artist_credit", Kind: Owned, Lifetime: Swept,
		Note: "Credit strings parsed from file tags. Orphan-swept when " +
			"no recording references them.",
	},
	{
		Name: "artist_credit_artist", Kind: Owned, Lifetime: Swept,
		Note: "Join table between credits and artists.",
	},
	{
		Name: "artist_enrichment", Kind: Derived, Lifetime: Retained,
		Note: "Which catalog enrichment passes have run for an artist. " +
			"Derived: losing it re-runs the fetches, which cost time and " +
			"someone else's rate limit but no data. Retained because a " +
			"stale mark for an artist no longer owned is harmless — the " +
			"backfill only ever asks about artists the library has.",
	},
	{
		Name: "artist_images", Kind: Cache, Lifetime: Swept,
		Note: "Artist photos from fanart.tv/MusicBrainz. Rows point at " +
			"files under the artist-images directory; the janitor sweeps " +
			"both together.",
	},
	{
		Name: "artist_metadata", Kind: Cache, Lifetime: Swept,
		Note: "Fetched artist bios and metadata. No TTL — swept when the " +
			"artist is no longer referenced.",
	},
	{
		Name: "artists", Kind: Owned, Lifetime: Swept,
		Note: "Artists parsed from file tags. Orphan-swept.",
	},
	{
		Name: "audio_files", Kind: Owned, Lifetime: Swept,
		Note: "MIXED KIND. Mostly an owned projection of files on disk, " +
			"but play_count, last_played and tag_status are authored and " +
			"exist nowhere else. Deleting a row to rebuild it destroys " +
			"that authored state — which is why a file rename currently " +
			"loses play counts. See the data architecture plan.",
	},
	{
		Name: "cover_art", Kind: Owned, Lifetime: Swept,
		Note: "Extracted embedded artwork. file_path names the original " +
			"only; the sized variants beside it are derived filenames and " +
			"must be expanded when deleting (see library.coverArtFileSet).",
	},
	{
		Name: "download_downloads", Kind: Authored, Lifetime: Cascade,
		Note: "One row per 'go find me this', i.e. one search-and-grab " +
			"attempt. Cascades from libraries, and cascades onward to " +
			"download_items. Terminal rows are history the user clears " +
			"explicitly.",
	},
	{
		Name: "download_items", Kind: Authored, Lifetime: Cascade,
		Note: "One grab attempt per row, with the ranked candidate stored " +
			"as JSON. Cascades from download_downloads. The candidate blob " +
			"is kept rather than re-derived because a provider's result " +
			"set is ephemeral — the peer that had the files may be gone, " +
			"and the row still has to explain why it was chosen.",
	},
	{
		Name: "download_providers", Kind: Authored, Lifetime: Retained,
		Note: "Download clients the user connected. Removed only by the " +
			"user. Holds no secrets: API keys live in a 0600 file keyed " +
			"by this row's id, so the table can be dumped into a bug " +
			"report without redaction.",
	},
	{
		Name: "download_requests", Kind: Authored, Lifetime: Cascade,
		Note: "The durable request list: one MBID per row, plus retry " +
			"bookkeeping. Cascades from libraries, and from a parent " +
			"artist request to the album requests it derived. Unlike " +
			"download_downloads these are not history — a request " +
			"outlives every download attempt made on it and is only " +
			"removed by the user or by the library coming to own what it " +
			"names.",
	},
	{
		Name: "explore_champion_fts", Kind: Cache, Lifetime: Retained, FTS: true,
		Note: "Full-text index over the champion entities of the " +
			"MusicBrainz dump. Rebuilt only by a full index build.",
	},
	{
		Name: "explore_index", Kind: Cache, Lifetime: Retained,
		Note: "The offline MusicBrainz search index. Rebuilding costs a " +
			"~205GB dump stream, so it is never swept automatically.",
	},
	{
		Name: "explore_index_fts", Kind: Cache, Lifetime: Retained, FTS: true,
		Note: "Full-text index over explore_index.",
	},
	{
		Name: "explore_index_meta", Kind: Cache, Lifetime: Retained,
		Note: "Build metadata for explore_index: dump version, coverage " +
			"tiers, last refresh.",
	},
	{
		Name: "excluded_paths", Kind: Authored, Lifetime: Cascade,
		Note: "Paths the user removed from the library, which the scanner " +
			"must not import again. Authored: it is a decision, not " +
			"derivable from disk. Cascades with its library, and a full " +
			"rescan clears it \u2014 the only way back for a path removed by " +
			"mistake.",
	},
	{
		Name: "file_types", Kind: Derived, Lifetime: Retained,
		Note: "Static lookup rows seeded from code, not user data.",
	},
	{
		Name: "genres", Kind: Owned, Lifetime: Swept,
		Note: "Genres parsed from file tags. Orphan-swept.",
	},
	{
		Name: "http_cache", Kind: Cache, Lifetime: Swept,
		Note: "Cached HTTP responses with a TTL. Reads filter on " +
			"expires_at; the janitor deletes expired rows.",
	},
	{
		Name: "job_state", Kind: Authored, Lifetime: Retained,
		Note: "Persisted background-job state, including scans the user " +
			"paused. Represents user intent, so it survives restarts.",
	},
	{
		Name: "libraries", Kind: Authored, Lifetime: Retained,
		Note: "The directories the user chose. Removed only by explicit " +
			"user action via RemoveLibrary.",
	},
	{
		Name: "lyrics_index", Kind: Derived, Lifetime: Retained, FTS: true,
		Note: "Full-text index over embedded and fetched lyrics. Rebuilt " +
			"from owned files plus the LRCLIB backfill.",
	},
	{
		Name: "play_history", Kind: Authored, Lifetime: Cascade,
		Note: "Listening history. Authored, but intentionally cascades " +
			"with its track — history for a file no longer in the library " +
			"has nothing to point at.",
	},
	{
		Name: "player_state", Kind: Authored, Lifetime: Retained,
		Note: "Volume, repeat and shuffle modes, last position.",
	},
	{
		Name: "playlist_tracks", Kind: Authored, Lifetime: SetNull,
		Note: "Playlist membership. Deliberately survives its track: " +
			"audio_file_id is nulled and phantom_* columns preserve the " +
			"entry so a rescan can re-link it.",
	},
	{
		Name: "playlists", Kind: Authored, Lifetime: Retained,
		Note: "User-created playlists, including smart playlist rules.",
	},
	{
		Name: "queue", Kind: Authored, Lifetime: Retained,
		Note: "The play queue's own state (current position, source).",
	},
	{
		Name: "queue_tracks", Kind: Authored, Lifetime: Cascade,
		Note: "Queue entries. Cascade with their track; the queue is " +
			"compacted afterwards.",
	},
	{
		Name: "recording_genres", Kind: Owned, Lifetime: Swept,
		Note: "Join table between recordings and genres.",
	},
	{
		Name: "recordings", Kind: Owned, Lifetime: Swept,
		Note: "Tracks as parsed from file tags. Orphan-swept.",
	},
	{
		Name: "release_group_recordings", Kind: Owned, Lifetime: Swept,
		Note: "Join table between release groups and recordings.",
	},
	{
		Name: "release_groups", Kind: Owned, Lifetime: Swept,
		Note: "Albums as parsed from file tags. Orphan-swept.",
	},
	{
		Name: "release_to_rg", Kind: Cache, Lifetime: Retained,
		Note: "Release to release-group mapping from the dump.",
	},
	{
		Name: "schema_migrations", Kind: Derived, Lifetime: Retained,
		Note: "Bookkeeping for sql/migrations: which numbered files have " +
			"run. Safe to lose — replaying an already-applied migration " +
			"tolerates its ALTER TABLE ADD COLUMN as a no-op and just " +
			"re-records it.",
	},
	{
		Name: "search_clicks", Kind: Authored, Lifetime: Retained,
		Note: "Which results the user picked, used to rank future " +
			"searches. Behavioural but unrecoverable if dropped.",
	},
	{
		Name: "search_index", Kind: Derived, Lifetime: Retained, FTS: true,
		Note: "Contentless FTS5 over the library. Individual rows cannot " +
			"be deleted, so stale entries are tolerated and filtered by " +
			"joining track_metadata; a full rescan rebuilds it.",
	},
	{
		Name: "similar_artist_map", Kind: Cache, Lifetime: Retained,
		Note: "Artist similarity edges derived from the dump.",
	},
	{
		Name: "tagging_candidates", Kind: Derived, Lifetime: Cascade,
		Note: "Scored MusicBrainz candidates for a tagging group. " +
			"Cascades with its tagging_items row.",
	},
	{
		Name: "tagging_items", Kind: Derived, Lifetime: Swept,
		Note: "MIXED KIND. The grouping is derived from folder layout, " +
			"but status and cleared_at record the user's review " +
			"decisions. Its library_id foreign key is NO ACTION, so " +
			"RemoveLibrary must delete these rows explicitly or the " +
			"library cannot be removed at all.",
	},
	{
		Name: "track_metadata", Kind: Derived, Lifetime: Retained,
		Note: "A view joining audio_files to its entity chain. Holds no " +
			"rows of its own.",
	},
}

// directories are the on-disk asset trees under the user data directory.
// Files there have no foreign keys, so nothing removes them implicitly —
// each needs a sweeper in the janitor.
var directories = []Directory{
	{
		Name: "covers", Kind: Derived,
		Note: "Extracted cover art plus generated _sm/_md/_lg variants. " +
			"Live set is cover_art.file_path expanded to its variants.",
	},
	{
		Name: "artist-images", Kind: Cache,
		Note: "Per-artist-MBID directories of fetched photos, a " +
			"primary.jpg with thumbnails, and a .miss marker for artists " +
			"known to have no art. Live set is artist_images.file_path.",
	},
	{
		Name: "cover-art-cache", Kind: Cache,
		Note: "Cover Art Archive images fetched for Explore browsing, " +
			"keyed by release-group MBID. Not tied to owned data at all.",
	},
}

// Directory is an on-disk asset tree owned by the application.
type Directory struct {
	// Name is the directory name under the user data directory.
	Name string
	// Kind is what the files cost to lose.
	Kind Kind
	// Note records what lives there and how the live set is determined.
	Note string
}

// Tables returns the full catalog.
func Tables() []Table {
	out := make([]Table, len(tables))
	copy(out, tables)

	return out
}

// Directories returns the catalogued asset directories.
func Directories() []Directory {
	out := make([]Directory, len(directories))
	copy(out, directories)

	return out
}

// Lookup returns the catalog entry for a table name, resolving FTS
// shadow tables to their parent.  Reports false for SQLite's internal
// tables and for anything not catalogued.
func Lookup(name string) (Table, bool) {
	if internalTables[name] {
		return Table{}, false
	}

	for _, t := range tables {
		if t.Name == name {
			return t, true
		}
	}

	if parent, ok := ftsParent(name); ok {
		return Lookup(parent)
	}

	return Table{}, false
}

// IsInternal reports whether a table is SQLite's own bookkeeping.
func IsInternal(name string) bool {
	return internalTables[name] || strings.HasPrefix(name, "sqlite_")
}

// ftsParent maps an FTS5 shadow table to the virtual table that owns it.
func ftsParent(name string) (string, bool) {
	for _, suffix := range ftsShadowSuffixes {
		if !strings.HasSuffix(name, suffix) {
			continue
		}

		parent := strings.TrimSuffix(name, suffix)

		for _, t := range tables {
			if t.Name == parent && t.FTS {
				return parent, true
			}
		}
	}

	return "", false
}

// ByKind returns every catalogued table of a given kind.
func ByKind(k Kind) []Table {
	var out []Table

	for _, t := range tables {
		if t.Kind == k {
			out = append(out, t)
		}
	}

	return out
}
