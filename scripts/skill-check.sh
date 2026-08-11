#!/usr/bin/env bash
#
# Every command in .pi/ is a `make` target on purpose: the Makefile is
# the source of truth for *how* to invoke something, and the skill only
# decides *which* and *in what order*.  This check keeps that honest —
# a renamed or deleted target turns into a failing commit rather than
# into an agent confidently running a command that no longer exists.
#
# It extracts every `make <target>` mentioned under .pi/ and asserts the
# target exists.  Usage:  scripts/skill-check.sh
set -euo pipefail

cd "$(dirname "$0")/.."

[ -d .pi ] || exit 0

# `make -pq` prints the database including every rule, without running
# anything.  It exits non-zero when a target is out of date, and under
# `pipefail` that would sink the whole assignment, so swallow it.
targets="$({ make -pqRr 2>/dev/null || true; } |
	awk '/^[a-zA-Z0-9][^$#\/\t=]*:([^=]|$)/ {sub(/:.*/, "", $0); print}' |
	sort -u)"

# A mention counts only when it is code: backticked (`make ui-test`) or
# the first thing on a line, as in a fenced block.  Bare prose is not
# scanned, because English says things like "a renamed make target".
mentioned="$(grep -rhoE '(`|^)make [a-z][a-z0-9-]*' .pi --include='*.md' |
	sed 's/^`//' | awk '{print $2}' | sort -u)"

missing=""

for t in $mentioned; do
	if ! printf '%s\n' "$targets" | grep -qx -- "$t"; then
		missing="$missing $t"
	fi
done

if [ -n "$missing" ]; then
	echo "skill-check: .pi/ documents make targets that do not exist:" >&2
	for t in $missing; do
		echo "  make $t" >&2
		grep -rln "make $t" .pi --include='*.md' | sed 's/^/      /' >&2
	done
	echo "Fix the docs, or restore the target." >&2
	exit 1
fi

echo "skill-check: $(printf '%s\n' "$mentioned" | wc -w) documented make targets, all present"
