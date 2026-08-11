#!/usr/bin/env bash
#
# Stop the headless app started by scripts/dev-headless.sh.
#
# Kills the saved process group, never `pkill -f`: a pkill pattern that
# appears in the invoking compound command's own command line matches
# the invoking shell, kills it, and silently drops everything after it.
#
# SIGTERM first, so OnBeforeClose / OnShutdown run and player and queue
# state are persisted — a seed built from an SIGKILLed app is a seed
# missing exactly the state those hooks write.
#
set -euo pipefail

cd "$(dirname "$0")/.."

PID_FILE=".dev/app.pid"
GRACE=10

if [ ! -f "$PID_FILE" ]; then
	echo "dev-stop: nothing running (no $PID_FILE)"
	exit 0
fi

PID="$(cat "$PID_FILE")"

if ! kill -0 "$PID" 2>/dev/null; then
	echo "dev-stop: pid $PID already gone"
	rm -f "$PID_FILE"
	exit 0
fi

# setsid made the app a process group leader, so -PID reaches the app,
# xvfb-run and the private dbus-daemon together.
kill -TERM -- "-$PID" 2>/dev/null || kill -TERM "$PID"

deadline=$((SECONDS + GRACE))
while kill -0 "$PID" 2>/dev/null; do
	if [ "$SECONDS" -ge "$deadline" ]; then
		echo "dev-stop: pid $PID ignored SIGTERM after ${GRACE}s, killing"
		kill -KILL -- "-$PID" 2>/dev/null || kill -KILL "$PID"
		break
	fi

	sleep 0.2
done

rm -f "$PID_FILE"
echo "dev-stop: stopped $PID"
