#!/bin/sh
# Snapshot the index build's database, which is the only copy of it.
#
# `/srv/yellowjacket/index-cache` is the `YJ_HOME` the index job keeps
# between runs (`.gitea/workflows/index-artifact.yml` mounts it at
# `/cache`).  Its catalog is *derived*, not downloaded: the only way to
# rebuild it is to re-stream the MetaBrainz dumps, which is hours at a
# rate that is someone else's to decide.  On 2026-08-17 a schema repair
# dropped it and cost exactly that.
#
# So it gets a snapshot, and this is the script a cron on that host runs.
# It is deliberately not part of the workflow: a backup that only exists
# while the thing it protects is being modified is not a backup.
#
# Usage (on the Gitea host):
#
#   scripts/index-cache-snapshot.sh [SOURCE_HOME] [DEST_DIR] [KEEP]
#
#   SOURCE_HOME  default /srv/yellowjacket/index-cache
#   DEST_DIR     default /srv/yellowjacket/index-snapshots
#   KEEP         how many to retain, default 2
#
# Suggested cron — daily, and nowhere near the Monday 04:00 build:
#
#   30 5 * * * /path/to/index-cache-snapshot.sh >> /var/log/yj-index-snapshot.log 2>&1
#
# Three things about it are load-bearing.
#
# **`VACUUM INTO`, not `cp`.** The database may be open, and a byte copy
# of a live SQLite file is a corrupt file with a plausible size.
# `VACUUM INTO` takes a read lock, writes a consistent compacted copy,
# and is safe while the index job is running — it costs the snapshot's
# own write, not the source's availability.
#
# **The staging directory is not copied.** `/cache/data/explore-staging`
# is a resumable checkpoint of work in flight; it is large, it changes
# constantly, and a build resumes without it. What cannot be re-derived
# cheaply is the finished catalog, which is in the database.
#
# **A snapshot that is not verified is a belief.** Each one is opened
# and asked for its catalog row count before the old ones are rotated
# out, so a run that produced an unreadable file leaves the previous
# good snapshot in place and fails loudly.
set -eu

SOURCE_HOME="${1:-/srv/yellowjacket/index-cache}"
DEST_DIR="${2:-/srv/yellowjacket/index-snapshots}"
KEEP="${3:-2}"

DB="$SOURCE_HOME/data/yj.db"
STAMP=$(date +%Y%m%d-%H%M%S)
OUT="$DEST_DIR/yj-index-$STAMP.db"

die() { echo "index-snapshot: $*" >&2; exit 1; }

command -v sqlite3 >/dev/null 2>&1 || die "sqlite3 is not installed"
[ -f "$DB" ] || die "no database at $DB (is SOURCE_HOME right?)"

mkdir -p "$DEST_DIR"

# Headroom: the copy is at most the size of the source, usually less
# (VACUUM compacts).  Refusing here beats a half-written snapshot.
need_kb=$(du -k "$DB" | cut -f1)
free_kb=$(df -Pk "$DEST_DIR" | awk 'NR == 2 { print $4 }')
[ "$free_kb" -gt "$need_kb" ] || die "not enough space in $DEST_DIR (need ~${need_kb}K, have ${free_kb}K)"

# A failed snapshot must leave nothing behind. `VACUUM INTO` refuses an
# existing file, so a partial one from a disk-full write would block
# every later run -- and worse, rotation counts files by name, so it
# would eventually be kept *instead of* a good one.
cleanup() { [ -n "${KEPT:-}" ] || rm -f "$OUT"; }
trap cleanup EXIT

echo "index-snapshot: $DB -> $OUT"
sqlite3 "$DB" "VACUUM INTO '$OUT'" || die "VACUUM INTO failed"

rows=$(sqlite3 "$OUT" "SELECT count(*) FROM explore_index" 2>/dev/null) \
	|| die "snapshot is unreadable: keeping the previous ones"
[ "${rows:-0}" -gt 0 ] || die "snapshot has an empty catalog: keeping the previous ones"

KEPT=1

echo "index-snapshot: ok, $rows catalog rows, $(du -h "$OUT" | cut -f1)"

# Rotate only after the new one has been verified.
ls -1t "$DEST_DIR"/yj-index-*.db 2>/dev/null | tail -n +"$((KEEP + 1))" | while read -r old; do
	echo "index-snapshot: removing $old"
	rm -f "$old"
done
