# Changing the database schema

The reasoning — why the local library is shaped like files rather than
like MusicBrainz, and what the metadata tables cost before they went —
is in `CLAUDE.md` under *Backend packages → database*. Read it once.
This is the checklist.

**There is one description of the schema and no migration chain.**
`sql/schemas/*.sql` declares the current shape; `applySchema` runs every
file on every open, and `CREATE ... IF NOT EXISTS` makes that idempotent.
`sql/migrations/`, `applyMigrations` and `schema_migrations` were
squashed away with plan 013. So:

**Adding a table or a column is one edit to one file.**

```bash
make generate            # sqlc + templ
go test ./backend/database/ ./backend/datamap/
make test
```

A new table has a second gate: **`backend/datamap`**. Add an entry
stating its Kind and Lifetime, or `TestCatalogCoversSchema` fails — and
if it is `Authored` and cascades, `TestAuthoredCascadesAreDeliberate`
wants an explicit exemption with a note, because authored data is what a
user cannot get back. If a *column* holds a different Kind from its
table (an authored flag on an owned projection, a fetched value beside a
tag-derived one), say so in the entry's note; `audio_files` and `lyrics`
are the worked examples.

**Existing databases are not migrated.** Nothing upgrades a database
from an older shape — delete your dev `YJ_HOME` and rescan, and rebuild
any seed you rely on (`make sandbox-seed NAME=default`). Revisit this
once real user databases exist in the wild.

**A stale one fails at the first query, not at open**, which is worth
knowing before you read the error. `applySchema` is
`CREATE TABLE IF NOT EXISTS`, so an old database keeps its old columns
and gains nothing; the app then starts fine and dies on
`no such column: title`. Every tier that does not *run the app* — unit
tests, `make ui-test`, `tsc` — is green while this is true, because
they build their database from the current schema. `make e2e` and
`make dev` are the two that will tell you, and only after the seed has
been rebuilt.

## The four ways this goes wrong

- **A query file must be ASCII.** sqlc's parameter rewriter works on
  byte offsets, so a single non-ASCII character in a *query* comment
  (an em dash, a curly quote) shifts every placeholder and generates
  garbage like `SELECid` — a parse error a long way from its cause.
  Schema files are not rewritten and may contain anything.
- **A slice and a named parameter do not compose.** `sqlc.slice`
  expands to N placeholders, but `sqlc.arg` is numbered independently,
  so the two in one query bind the wrong values —
  `GetFilePathsByAlbums([1,2], 0)` read album id 2 as the library id.
  Where a query needs both, return the column and filter in Go.
- **A write wearing a query's shape still needs the writer.**
  `QueryContext`/`QueryRow` route to the query-only read pool, so an
  `INSERT ... RETURNING` through one fails at runtime with "attempt to
  write a readonly database (8)". Use `ExecContext`, or
  `QueryRowWriter`. `TestNoWritesOnTheReadPool` walks the tree for it.
- **A view is dropped and recreated.** `CREATE VIEW IF NOT EXISTS`
  no-ops against a database holding the old definition, so
  `track_metadata.sql` opens with `DROP VIEW IF EXISTS`.

## Where things go

New queries go in `backend/database/sql/queries/`; generated Go lands in
`backend/database/sql/sqlcgen/`, which is never edited by hand. Anything
returning a track selects from the `track_metadata` view rather than
re-joining — that is why there is one row type and one mapper.

Tests use `database.NewTestDB(t)`, built by the same `applySchema`
production uses, and seed rows with `database.InsertTestTrack(t, db,
database.TestTrack{...})` rather than assembling inserts by hand.
