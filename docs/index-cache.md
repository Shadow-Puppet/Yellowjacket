# The index cache, and why it has a snapshot

`/srv/yellowjacket/index-cache` on the Gitea host is the `YJ_HOME` the
search-index job keeps between runs — `.gitea/workflows/index-artifact.yml`
mounts it at `/cache`. It holds the catalog every user eventually
downloads, and it is the one database in this project that is
**derived rather than downloaded**.

That is the whole reason this document exists. An install with a broken
catalog re-fetches the ~0.6 GB artifact and is fine in a minute. This
database *is* what that artifact is cut from, so its only route back is
re-streaming the MetaBrainz dumps: hours, at a rate that belongs to
someone else's server, holding a runner of capacity 1 the entire time.

## What happened on 2026-08-17

A schema repair (`fix(database): retire a table whose shape the schema
moved past`) dropped every table whose live shape disagreed with the
schema, before `applySchema`. Correct for the app. Applied here it
deleted the catalog 19 seconds into the first run:

```
retiring a table ... table=explore_index
  reason="column entity_type is TEXT, schema declares INTEGER"
index maintenance mode=build reason="no completed import yet"
```

The mismatch was real and deliberate: this database is kept in the older
text encoding, which `artifactStoresText` and `sourceColumns` exist to
tolerate. So it would have been judged stale on *every* run.

Two things came out of it. `retireStaleCache` is now a build tag —
false under `indexbuild`, true in the app — and
`TestNoCacheTableIsRetiredHere` asserts the outcome rather than the
mechanism, so the next destructive repair fails a test instead of a
production volume. And the volume got the snapshot it should always have
had, below.

## Taking snapshots

```sh
scripts/index-cache-snapshot.sh [SOURCE_HOME] [DEST_DIR] [KEEP]
```

Defaults: `/srv/yellowjacket/index-cache`, `/srv/yellowjacket/index-snapshots`,
keep 2. On the Gitea host, daily and away from the Monday 04:00 build:

```
30 5 * * * /path/to/index-cache-snapshot.sh >> /var/log/yj-index-snapshot.log 2>&1
```

Three properties worth knowing before trusting it:

- **It uses `VACUUM INTO`, not `cp`.** The database may be open, and a
  byte copy of a live SQLite file is a corrupt file of plausible size.
  `VACUUM INTO` takes a read lock and writes a consistent, compacted
  copy; it is safe to run while a build is in progress.
- **It does not copy `data/explore-staging`.** That is a resumable
  checkpoint of work in flight — large, constantly changing, and a build
  resumes without it. What cannot be cheaply re-derived is the finished
  catalog, which is in the database.
- **It verifies before it rotates.** Each snapshot is reopened and asked
  for its catalog row count; a run that produces an unreadable or empty
  file fails loudly, deletes its own output, and leaves the previous
  snapshots alone. Both paths are exercised, not assumed.

## Restoring

Stop anything that might be using the volume first — the job holds it
for the length of a build, and the concurrency group (`search-index`)
means a queued run will start the moment one ends.

```sh
cd /srv/yellowjacket
mv index-cache/data/yj.db index-cache/data/yj.db.broken   # keep it until you are sure
cp index-snapshots/yj-index-<stamp>.db index-cache/data/yj.db
chown --reference=index-cache/data/yj.db.broken index-cache/data/yj.db
```

Then dispatch the workflow with `mode=auto`. A restored snapshot is
older than the dumps, so `indexbuild` resolves to `refresh` and folds in
the incremental listens since — which is minutes, not hours.

Two notes on what a restore does *not* need. The staging directory can
be deleted; it will be rebuilt if a build is needed. And the published
artifact is untouched by any of this: users keep downloading the last
good one until a run reports `complete=true` and `changed=true`
republishes.

## The trade this leaves open

With Cache tables no longer retired under `indexbuild`, a future
`explore_index` column change will fail this job **loudly** — at
`applySchema`, or at the first query naming the column — rather than
silently rebuilding. That is the right default: loud is recoverable and
a silent day of downloading is not. It does mean the next schema change
touching `explore_index` needs a deliberate plan for this one database:
take a snapshot, apply the change to a copy, or accept a rebuild
knowingly.
