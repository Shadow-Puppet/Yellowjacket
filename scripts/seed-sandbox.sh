#!/usr/bin/env bash
#
# Build a seeded YJ_HOME snapshot by RUNNING THE APP.
#
# The point of a seed is to start a harness run *inside* the app rather
# than on the first-run wizard, which intercepts every pointer event
# until a library exists.  The wizard's dismissal condition is not a
# config file — it is `GetAllLibrariesWithTrackCounts()` returning a
# non-empty list — so the only honest way to produce that state is to
# call the real AddLibrary binding and let the real scanner finish.
#
# Hand-writing a config.toml and DB rows would be a second description
# of a valid YJ_HOME, free to drift from what the app actually writes.
# That is the failure mode .planning/NOTES.md records for the old
# migration chain, and it is not worth repeating for seeds.
#
# Usage:
#   scripts/seed-sandbox.sh [--name NAME] [--port N] [--no-build]
#
set -euo pipefail

cd "$(dirname "$0")/.."

REPO_ROOT="$PWD"
RUN_DIR="$REPO_ROOT/.dev"
SEED_DIR="$RUN_DIR/seeds"
LOG_FILE="$RUN_DIR/app.log"
MANIFEST="$REPO_ROOT/test_data/music_library_test.manifest.json"

NAME="default"
PORT=34115
SESSION="yj-seed"
BUILD_ARGS=()
SCAN_TIMEOUT=180

while [ $# -gt 0 ]; do
	case "$1" in
	--name)
		NAME="${2:?--name needs a value}"
		shift 2
		;;
	--port)
		PORT="${2:?--port needs a number}"
		shift 2
		;;
	--no-build)
		BUILD_ARGS+=(--no-build)
		shift
		;;
	*)
		echo "seed-sandbox: unknown argument: $1" >&2
		exit 2
		;;
	esac
done

need() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "seed-sandbox: $1 not found in PATH" >&2
		exit 1
	}
}
need playwright-cli
need jq

if [ ! -f "$MANIFEST" ]; then
	echo "seed-sandbox: fixtures missing; run 'make testdata'" >&2
	exit 1
fi

LIBRARY_DIR="$REPO_ROOT/$(jq -r .libraryRoot "$MANIFEST")"
WANT_TRACKS="$(jq '.tracks | length' "$MANIFEST")"
FIXTURE_HASH="$(jq -r .hash "$MANIFEST")"

cleanup() {
	playwright-cli -s="$SESSION" close >/dev/null 2>&1 || true
	./scripts/dev-stop.sh >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "seed-sandbox: building '$NAME' from $WANT_TRACKS fixture tracks"

# Start on an empty YJ_HOME: seeding must exercise the same first-run
# path a real install takes.
#
# YJ_CORE_INDEX_URL points at a dead address on purpose.  A seed must
# not reach for the real explore artifact — that is a minute of network
# per seed, and it makes the result depend on what the artifact server
# happened to be serving that day.
YJ_CORE_INDEX_URL="http://127.0.0.1:1/none.tar.zst" \
	./scripts/dev-headless.sh --fresh --port "$PORT" "${BUILD_ARGS[@]}"

YJ_HOME="$(cat "$RUN_DIR/app.home")"

playwright-cli -s="$SESSION" open "http://localhost:$PORT" >/dev/null

# Every binding call gets a timeout.  A call with wrong argument types
# makes the backend log 'error parsing arguments' and never fire the
# callback, so the in-page promise never settles and a naive await
# hangs forever.
call() {
	playwright-cli -s="$SESSION" eval "async () => {
		const timeout = new Promise((_, reject) =>
			setTimeout(() => reject(new Error('binding timeout')), 15000));
		return await Promise.race([(async () => { $1 })(), timeout]);
	}"
}

echo "seed-sandbox: registering library $LIBRARY_DIR"

if ! call "return await window.go.library.Library.AddLibrary(
	${LIBRARY_DIR@Q});" >/dev/null; then
	echo "seed-sandbox: AddLibrary failed; app log:" >&2
	tail -n 40 "$LOG_FILE" >&2
	exit 1
fi

# Wait on the observable outcome — the track count the app itself
# reports — rather than on a fixed sleep or a scan event.  This also
# validates the manifest against the real scanner: if the two disagree,
# a fixture is not being ingested and the seed is wrong.
echo "seed-sandbox: waiting for the scan to reach $WANT_TRACKS tracks"

# The result is tagged rather than scraped for bare digits:
# playwright-cli echoes the evaluated source back, and that source
# contains numbers of its own (the binding timeout, for one).
deadline=$((SECONDS + SCAN_TIMEOUT))
got=0

while [ "$SECONDS" -lt "$deadline" ]; do
	got="$(call "const libs =
		await window.go.library.Library.GetAllLibrariesWithTrackCounts();
		const total = (libs ?? []).reduce(
			(n, l) => n + (l.trackCount ?? 0), 0);
		return 'YJTRACKS' + '=' + total;" |
		grep -oE 'YJTRACKS=[0-9]+' | head -n 1 | cut -d= -f2)"
	got="${got:-0}"

	[ "$got" = "$WANT_TRACKS" ] && break

	sleep 1
done

if [ "$got" != "$WANT_TRACKS" ]; then
	echo "seed-sandbox: scan settled at $got/$WANT_TRACKS tracks" >&2
	echo "  (a fixture the scanner rejects, or a scan still running)" >&2
	tail -n 40 "$LOG_FILE" >&2
	exit 1
fi

playwright-cli -s="$SESSION" close >/dev/null 2>&1 || true

# SIGTERM, so OnBeforeClose / OnShutdown persist window, player and
# queue state.  A seed built from a killed app is missing exactly the
# state those hooks write.
./scripts/dev-stop.sh

mkdir -p "$SEED_DIR"
tar -cf "$SEED_DIR/$NAME.tar" -C "$YJ_HOME" .

cat >"$SEED_DIR/$NAME.json" <<EOF
{
  "name": "$NAME",
  "tracks": $WANT_TRACKS,
  "libraryRoot": "$LIBRARY_DIR",
  "fixtureHash": "$FIXTURE_HASH",
  "createdAt": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
EOF

trap - EXIT

echo "seed-sandbox: wrote $SEED_DIR/$NAME.tar ($WANT_TRACKS tracks)"
echo "  use it with: make dev-headless SEED=$NAME"
