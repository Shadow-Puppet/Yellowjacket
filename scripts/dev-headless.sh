#!/usr/bin/env bash
#
# Start YellowJacket headless, in the background, and return.
#
# `wails dev` binds an HTTP + WebSocket dev server on :34115 that serves
# the real frontend with the real generated bindings on `window.go` and
# bridges every call and every runtime.EventsEmit to the *same* Go
# backend the desktop window uses.  A browser pointed at it is not a
# mock.  That is what makes this app drivable by a coding agent.
#
# Three details are load-bearing:
#
#   * We run the dev *binary*, not `wails dev`.  app_dev.go parses
#     -devserver / -assetdir / -loglevel straight from os.Args, so a
#     `go build -tags "dev webkit2_41"` binary serves the identical
#     devserver with no file watcher, no rebuild supervisor and no
#     reload broadcast: one process, one PID, deterministic startup.
#
#   * Xvfb is not optional.  devserver.Run ends in Frontend.Run(ctx),
#     which opens the GTK window and blocks; no flag suppresses it.
#
#   * dbus-run-session is not incidental.  A private session bus makes
#     backend/mediacontrols register MPRIS for real, so it becomes
#     assertable with busctl.  It replaces the bus, not /run/user, so
#     PulseAudio still works and InitSpeaker succeeds.
#
# Usage:
#   scripts/dev-headless.sh [--seed NAME|--fresh] [--port N] [--no-build]
#
set -euo pipefail

cd "$(dirname "$0")/.."

REPO_ROOT="$PWD"
RUN_DIR="$REPO_ROOT/.dev"
PID_FILE="$RUN_DIR/app.pid"
LOG_FILE="$RUN_DIR/app.log"
HOME_FILE="$RUN_DIR/app.home"
SEED_DIR="$RUN_DIR/seeds"
BIN="$REPO_ROOT/build/bin/yj-dev"

PORT=34115
SEED=""
FRESH=0
BUILD=1
LOG_LEVEL="${YJ_LOG_LEVEL:-debug}"
STARTUP_TIMEOUT=60

usage() {
	sed -n '3,28p' "$0" | sed 's/^# \{0,1\}//'
	exit "${1:-0}"
}

while [ $# -gt 0 ]; do
	case "$1" in
	--seed)
		SEED="${2:?--seed needs a name}"
		shift 2
		;;
	--fresh)
		FRESH=1
		shift
		;;
	--port)
		PORT="${2:?--port needs a number}"
		shift 2
		;;
	--no-build)
		BUILD=0
		shift
		;;
	-h | --help) usage 0 ;;
	*)
		echo "dev-headless: unknown argument: $1" >&2
		usage 2
		;;
	esac
done

mkdir -p "$RUN_DIR"

# ── Refuse to stack instances ────────────────────────────────────────
# Two backends on one port fails obscurely; two backends on one YJ_HOME
# corrupts a SQLite database.  Check the saved PID, not the port.
if [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
	echo "dev-headless: already running (pid $(cat "$PID_FILE")); " \
		"run 'make dev-stop' first" >&2
	exit 1
fi
rm -f "$PID_FILE"

# ── Choose the YJ_HOME ───────────────────────────────────────────────
# A seed is a YJ_HOME that a previous run of the app produced, tarred
# up (see scripts/seed-sandbox.sh).  Restoring it means starting *in*
# the app instead of on the first-run wizard, which intercepts every
# pointer event until a library exists.
if [ -n "$SEED" ] && [ "$FRESH" = 1 ]; then
	echo "dev-headless: --seed and --fresh are mutually exclusive" >&2
	exit 2
fi

if [ -n "$SEED" ]; then
	SEED_TAR="$SEED_DIR/$SEED.tar"
	if [ ! -f "$SEED_TAR" ]; then
		echo "dev-headless: no seed '$SEED' at $SEED_TAR" >&2
		echo "  build one with: make sandbox-seed NAME=$SEED" >&2
		exit 1
	fi

	YJ_HOME="$RUN_DIR/home-$SEED"
	rm -rf "$YJ_HOME"
	mkdir -p "$YJ_HOME"
	tar -xf "$SEED_TAR" -C "$YJ_HOME"
	echo "dev-headless: restored seed '$SEED'"
elif [ "$FRESH" = 1 ]; then
	# Deliberately empty: the first-run wizard is itself a surface
	# that needs testing.
	YJ_HOME="$RUN_DIR/home-fresh"
	rm -rf "$YJ_HOME"
	mkdir -p "$YJ_HOME"
	echo "dev-headless: fresh YJ_HOME (expect the first-run wizard)"
else
	YJ_HOME="${YJ_HOME:-$RUN_DIR/home}"
	mkdir -p "$YJ_HOME"
fi

export YJ_HOME
echo "$YJ_HOME" >"$HOME_FILE"

# ── Build ────────────────────────────────────────────────────────────
if [ "$BUILD" = 1 ]; then
	echo "dev-headless: building frontend + dev binary..."
	(cd frontend && pnpm install --silent && pnpm build >/dev/null)
	go build -tags "dev webkit2_41" -o "$BIN" .
fi

if [ ! -x "$BIN" ]; then
	echo "dev-headless: $BIN missing; drop --no-build" >&2
	exit 1
fi

# ── Launch ───────────────────────────────────────────────────────────
# setsid puts the app in its own process group so dev-stop can kill the
# whole tree (xvfb-run, dbus-daemon, the app) by group id.  Never
# `pkill -f`: the pattern matches the invoking shell's own command line
# and silently drops the rest of the chain.
: >"$LOG_FILE"

# YJ_TESTCTL mounts backend/testctl's /__test/ endpoints.  It is opt-in
# rather than implied by the dev build so that a human's `make dev` does
# not carry an arbitrary-SQL endpoint on a listening port.
YJ_TESTCTL=1 \
	YJ_LOG_LEVEL="$LOG_LEVEL" setsid dbus-run-session -- xvfb-run -a \
	"$BIN" \
	-devserver "localhost:$PORT" \
	-assetdir "$REPO_ROOT/frontend/dist" \
	-loglevel Debug \
	>>"$LOG_FILE" 2>&1 &

APP_PID=$!
echo "$APP_PID" >"$PID_FILE"

# ── Wait for the dev server ──────────────────────────────────────────
deadline=$((SECONDS + STARTUP_TIMEOUT))
until curl -sf -o /dev/null "http://localhost:$PORT/"; do
	if ! kill -0 "$APP_PID" 2>/dev/null; then
		echo "dev-headless: app exited during startup; last log lines:" >&2
		tail -n 30 "$LOG_FILE" >&2
		rm -f "$PID_FILE"
		exit 1
	fi

	if [ "$SECONDS" -ge "$deadline" ]; then
		echo "dev-headless: :$PORT did not answer within ${STARTUP_TIMEOUT}s" >&2
		tail -n 30 "$LOG_FILE" >&2
		exit 1
	fi

	sleep 0.25
done

cat <<EOF
dev-headless: up
  url      http://localhost:$PORT
  pid      $APP_PID
  YJ_HOME  $YJ_HOME
  log      ${LOG_FILE#"$REPO_ROOT"/}   (make dev-logs)

Drive it with:
  playwright-cli -s=yj open http://localhost:$PORT
  playwright-cli -s=yj snapshot
  playwright-cli -s=yj eval "() => window.__yjEvents.call('queue.Queue.GetState', [], 5000)"

A binding call that never settles means wrong argument types: the
backend logs 'error parsing arguments' and never fires the callback.
The app log is the only place that shows up, so always use a timeout.
EOF
