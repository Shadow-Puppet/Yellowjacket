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
#                           [--manifest PATH]
#
# --manifest points at a different generated library's manifest, which
# is how the bulk measurement library (`make bulkdata`) gets seeded:
# same script, same discipline, different pile of files.  A manifest
# either lists its tracks or states a trackCount; both are accepted,
# because describing 50 000 tracks individually would serve nobody.
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
BUILD_ARGS=()
# Replaced below once the manifest says how many tracks are coming: a
# 50 000-track scan is minutes, and a fixed 180 s deadline would abort
# a healthy run rather than an unhealthy one.
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
	--manifest)
		MANIFEST="${2:?--manifest needs a path}"
		shift 2
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
need curl
need jq

if [ ! -f "$MANIFEST" ]; then
	echo "seed-sandbox: no manifest at $MANIFEST" >&2
	echo "  run 'make testdata' (fixtures) or 'make bulkdata' (measurement)" >&2
	exit 1
fi

LIBRARY_DIR="$REPO_ROOT/$(jq -r .libraryRoot "$MANIFEST")"
WANT_TRACKS="$(jq '.trackCount // (.tracks | length)' "$MANIFEST")"
SCAN_TIMEOUT=$((180 + WANT_TRACKS / 50))
FIXTURE_HASH="$(jq -r .hash "$MANIFEST")"

cleanup() {
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

# A binding is called over the runtime's own HTTP endpoint, by method
# name — the same request the bundle makes, minus the browser.
#
# This used to drive a real page through playwright-cli, because v2's
# only way in was `window.go`.  v3 answers the same call over HTTP, so a
# browser (and a global npm install of the CLI, in CI) buys nothing
# here: seeding needs the *app* to run and the *real* scanner to finish,
# which it still does. It also fails properly now — v3 answers a bad
# argument with a 422 and a TypeError naming it, where v2 logged
# "error parsing arguments" and never fired the callback.
call() {
	local method="$1"
	local args="${2:-[]}"
	local body

	body="$(jq -nc --arg m "yellowjacket/backend/$method" --argjson a "$args" \
		'{object: 0, method: 0, args: {"call-id": "seed", methodName: $m, args: $a}}')"

	curl -sS --fail-with-body --max-time 30 \
		-X POST "http://localhost:$PORT/wails/runtime" \
		-H 'Content-Type: application/json' \
		-d "$body"
}

echo "seed-sandbox: registering library $LIBRARY_DIR"

if ! call library.Library.AddLibrary \
	"$(jq -nc --arg d "$LIBRARY_DIR" '[$d]')" >/dev/null; then
	echo "seed-sandbox: AddLibrary failed; app log:" >&2
	tail -n 40 "$LOG_FILE" >&2
	exit 1
fi

# Wait on the observable outcome — the track count the app itself
# reports — rather than on a fixed sleep or a scan event.  This also
# validates the manifest against the real scanner: if the two disagree,
# a fixture is not being ingested and the seed is wrong.
echo "seed-sandbox: waiting for the scan to reach $WANT_TRACKS tracks"

deadline=$((SECONDS + SCAN_TIMEOUT))
got=0

while [ "$SECONDS" -lt "$deadline" ]; do
	got="$(call library.Library.GetAllLibrariesWithTrackCounts |
		jq '[(. // [])[].trackCount // 0] | add // 0')"
	got="${got:-0}"

	[ "$got" = "$WANT_TRACKS" ] && break

	# A large scan is minutes of silence otherwise, which is
	# indistinguishable from a hang.
	if [ $((SECONDS % 15)) -eq 0 ]; then
		echo "  ... $got/$WANT_TRACKS"
	fi

	sleep 1
done

if [ "$got" != "$WANT_TRACKS" ]; then
	echo "seed-sandbox: scan settled at $got/$WANT_TRACKS tracks" >&2
	echo "  (a fixture the scanner rejects, or a scan still running)" >&2
	tail -n 40 "$LOG_FILE" >&2
	exit 1
fi

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
