# Changing the database schema

The reasoning — why there are two files, what the old 48-step migration
chain got wrong, and when squashing is legitimate — is in `CLAUDE.md`
under *Backend packages → database*. Read it once. This is the
checklist.

**A brand-new table needs one file, not two.** The rule below is about
a *column added to a table that already exists*. `applySchema` runs
every file in `sql/schemas/` on every open, so a
`CREATE TABLE IF NOT EXISTS` reaches an existing install verbatim and a
migration for it would be a second description of the same table — the
thing the third rule forbids. Its indexes go in the schema file too,
because the column and the index arrive together.

A new table has a second gate: **`backend/datamap`**. Add an entry
stating its Kind and Lifetime, or `TestCatalogCoversSchema` fails — and
if it is `Authored` and cascades, `TestAuthoredCascadesAreDeliberate`
wants an explicit exemption with a note, because authored data is what
a user cannot get back.

Adding a **column** to an existing table needs **two** files, not one:

1. **`backend/database/sql/schemas/*.sql`** — `CREATE TABLE ... IF NOT
   EXISTS`, the literal target shape, what sqlc reads and what a fresh
   install gets verbatim. Add the new column **last** in the
   `CREATE TABLE`.
2. **`backend/database/sql/migrations/NNNN_description.sql`** — the
   `ALTER TABLE ... ADD COLUMN` (and any index on it) that gets an
   existing database to the same shape. Schema files are a no-op against
   a table that already exists, so without this an upgrade never gets
   the column.

Then:

```bash
make generate                                        # sqlc + templ
go test ./backend/database/         # migration + column-order tests
make test
```

Rebuild any seed you rely on (`make sandbox-seed NAME=default`) and
delete your own dev `YJ_HOME` if you want to see the fresh-install path
rather than the migrated one.

## The three ways this goes wrong

- **Column order must match between the two paths.** `ADD COLUMN`
  always appends, so a migrated column declared anywhere but last in
  `CREATE TABLE` leaves fresh and upgraded installs disagreeing on
  order — and sqlc binds `SELECT *` positionally, so one of them
  silently reads the wrong field.
  `TestMigrations_ColumnOrderMatchesFreshInstall` is the regression test.
- **Do not put an index on a migrated column in `sql/schemas/`.**
  Schema files run *before* migrations, against a database that may not
  have the column yet, and the predicate fails. Declare the index in the
  migration, after the `ALTER TABLE`.
- **Do not add a third description of the schema anywhere.** A
  migration's `ADD COLUMN` failing with "duplicate column name" against
  an already-current database is expected and tolerated, not an error to
  route around.

New queries go in `backend/database/sql/queries/`; generated Go lands in
`backend/database/sql/sqlcgen/`, which is never edited by hand. Tests
use `database.NewTestDB(t)`, built by the same `applySchema` production
uses, so the two cannot diverge.
