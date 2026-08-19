#!/usr/bin/env bash
#
# Start YellowJacket headless, in the background, and return.
#
# Wails v3's server mode binds an HTTP server on :34115 that serves the
# real frontend with the real generated bindings and bridges every call
# and every event to the *same* Go backend a desktop window would use.
# A browser pointed at it is not a mock.  That is what makes this app
# drivable by a coding agent.
#
# Three details are load-bearing:
#
#   * We build with `-tags dev,server` and run the binary.  This is a
#     first-class Wails mode ("a pure HTTP server without native GUI
#     dependencies"), not something hand-rolled: v2 had no such thing,
#     so this script used to run a dev binary whose app_dev.go parsed
#     -devserver / -assetdir out of os.Args.  That file is gone with v2.
#     The port comes from WAILS_SERVER_PORT.
#
#   * Xvfb is gone, and that is the point of server mode.  v2's
#     devserver.Run ended in Frontend.Run(ctx), which opened the GTK
#     window and blocked with no flag to suppress it; server mode opens
#     no window, so there is no display to fake.
#
#   * dbus-run-session is not incidental.  A private session bus makes
#     backend/mediacontrols register MPRIS for real, so it becomes
#     assertable with busctl.  It replaces the bus, not /run/user, so
#     PulseAudio still works and InitSpeaker succeeds.
#
#   * The frontend is *embedded*, not served from disk.  main.go has
#     //go:embed all:frontend/dist, so a frontend change needs the
#     rebuild this script does anyway; there is no -assetdir to point
#     at a live directory.
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
	sed -n '3,38p' "$0" | sed 's/^# \{0,1\}//'
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

# ── Refuse to inherit somebody else's port ───────────────────────────
# The PID check above only knows about *this* worktree: `make dev-stop`
# kills the pid in this .dev/app.pid and nothing else.  Several worktrees
# of this repo share the default port, so an app orphaned by a deleted
# worktree goes on listening with nothing left to stop it.
#
# Without this check the new app starts, fails to bind, exits — and every
# curl and playwright-cli call afterwards goes to the *other* process, so
# the harness reports facts about an app nobody asked for.  That is not a
# quiet wrongness either: it presented as
# "no such table: libraries" against a freshly created YJ_HOME, which
# reads exactly like applySchema or staleshape.go having gone wrong and
# is a frightening place to start looking.
#
# The startup wait below cannot catch it, because the health check is
# satisfied by *any* app on the port — which is precisely the failure.
# So it is refused here, before anything is launched, rather than warned
# about.  --port already exists for the legitimate second-app case.
port_holder() {
	command -v ss >/dev/null || return 0
	ss -lptn "sport = :$PORT" 2>/dev/null | grep -oP 'pid=\K[0-9]+' | head -n 1
}

if curl -sf -o /dev/null --max-time 2 "http://localhost:$PORT/" ||
	[ -n "$(port_holder)" ]; then
	holder="$(port_holder)"
	echo "dev-headless: :$PORT is already in use; refusing to start" >&2
	if [ -n "$holder" ]; then
		# /proc/<pid>/cwd names the checkout it belongs to, and says
		# "(deleted)" for the orphaned-worktree case that is the whole
		# reason this is worth a check.
		cwd="$(readlink "/proc/$holder/cwd" 2>/dev/null || echo unknown)"
		cmd="$(tr '\0' ' ' <"/proc/$holder/cmdline" 2>/dev/null || echo unknown)"
		echo "  pid  $holder  ($cmd)" >&2
		echo "  cwd  $cwd" >&2
		# The PID-file check above has already passed, so whatever this
		# is, `make dev-stop` does not know about it — saying otherwise
		# sends you to a command that will report success and change
		# nothing.  Never `pkill -f` here either: the pattern would
		# match this script's own command line.
		echo "  'make dev-stop' will not touch it (it is not in" >&2
		echo "  ${PID_FILE#"$REPO_ROOT"/}): kill $holder, or pass --port." >&2
	else
		echo "  The holder could not be identified (no ss, or it belongs" >&2
		echo "  to another user). Try: ss -lptn 'sport = :$PORT'" >&2
	fi
	exit 1
fi

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
	echo "dev-headless: building frontend + dev server binary..."
	(cd frontend && pnpm install --silent && pnpm build >/dev/null)
	go build -tags "dev,server" -o "$BIN" .
fi

if [ ! -x "$BIN" ]; then
	echo "dev-headless: $BIN missing; drop --no-build" >&2
	exit 1
fi

# ── Launch ───────────────────────────────────────────────────────────
# setsid puts the app in its own process group so dev-stop can kill the
# whole tree (dbus-daemon, the app) by group id.  Never
# `pkill -f`: the pattern matches the invoking shell's own command line
# and silently drops the rest of the chain.
: >"$LOG_FILE"

# YJ_TESTCTL mounts backend/testctl's /__test/ endpoints.  It is opt-in
# rather than implied by the dev build so that a human's `make dev` does
# not carry an arbitrary-SQL endpoint on a listening port.
#
# YJ_CORE_INDEX_URL points at a dead address, which is what
# seed-sandbox.sh and ci.yml already do and what this script was the
# only one *not* doing.  Without it the app downloads and builds the
# real ~1M-row Explore catalog into the run's YJ_HOME, so a local `make
# e2e` runs against a different world than CI: the specs that stage
# their own catalog rows (requested-badge) then search a catalog full
# of real albums, fail to find their fixture, and report it as a
# regression in whatever was last changed.  A spec tier whose result
# depends on what a previous run downloaded is not a result -- the same
# rule as the emulator's `-no-snapshot`.
#
# Set YJ_CORE_INDEX_URL yourself to opt back in, for exploring Explore
# by hand.
YJ_TESTCTL=1 \
	YJ_CORE_INDEX_URL="${YJ_CORE_INDEX_URL:-http://127.0.0.1:1/none.tar.zst}" \
	WAILS_SERVER_PORT="$PORT" \
	YJ_LOG_LEVEL="$LOG_LEVEL" setsid dbus-run-session -- \
	"$BIN" \
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

# The loop above exits on the first answer from the port, and "something
# answered" is not "the app we started answered".  The pre-launch guard
# makes that unlikely rather than impossible — a race, or a listener
# started in between — and the check is one signal, so it is worth making
# here too.  An empty log beside a dead pid is the "it exited immediately
# and nothing said so" case that the original report spent its time on.
if ! kill -0 "$APP_PID" 2>/dev/null; then
	echo "dev-headless: :$PORT answered, but the app we started (pid" >&2
	echo "  $APP_PID) is gone — something else holds the port." >&2
	tail -n 30 "$LOG_FILE" >&2
	rm -f "$PID_FILE"
	exit 1
fi

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

A bad binding call now rejects rather than hanging: v3 answers wrong
argument types with a TypeError naming the argument and an unknown
method with a ReferenceError. The timeout in __yjEvents.call is a
backstop for a hung request, not how a mistake becomes visible.
EOF
