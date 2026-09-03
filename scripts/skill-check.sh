#!/usr/bin/env bash
#
# Every command in the agent-facing docs is a `make` target on purpose:
# the Makefile is the source of truth for *how* to invoke something, and
# the docs only decide *which* and *in what order*.  This check keeps
# that honest — a renamed or deleted target turns into a failing commit
# rather than into an agent confidently running a command that no longer
# exists.
#
# It checks two things.  Usage:  scripts/skill-check.sh
#
# **Every `make <target>` named in an agent-facing doc exists.**  The
# scanned set is `.pi/` *and* CLAUDE.md, which is the half that was
# missing: CLAUDE.md names 27 targets and nothing verified one of them,
# so the file the agents trust most was the file least checked.
#
# README.md and CONTRIBUTING.md are in it too, and the header sentence
# above is why: a person who has *not* read the Makefile goes looking in
# the contributor-facing doc, so a renamed target sends them off the
# same cliff it sends an agent off.  CONTRIBUTING.md names 21 targets.
#
# **AGENTS.md is a symlink to CLAUDE.md.**  This repo is worked on by
# two agent harnesses that read different files by convention — Claude
# Code reads CLAUDE.md, others read AGENTS.md — and two harnesses
# reading two descriptions of one project is how they come to hold
# different beliefs about it.  A symlink makes that impossible by
# construction; a *copy* would pass every other check in this repo while
# silently drifting, which is exactly the failure being prevented, so
# the symlink itself is asserted rather than its contents compared.
set -euo pipefail

cd "$(dirname "$0")/.."

# The symlink half runs even without .pi/, since it is not about .pi/.
if [ -e AGENTS.md ] || [ -L AGENTS.md ]; then
	if [ ! -L AGENTS.md ]; then
		echo "skill-check: AGENTS.md is a regular file, not a symlink to CLAUDE.md." >&2
		echo "  Two harnesses would read two descriptions of one project." >&2
		echo "  Fix:  rm AGENTS.md && ln -s CLAUDE.md AGENTS.md" >&2
		exit 1
	fi

	target="$(readlink AGENTS.md)"

	if [ "$target" != "CLAUDE.md" ]; then
		echo "skill-check: AGENTS.md points at '$target', expected CLAUDE.md." >&2
		exit 1
	fi
fi

# The scan is over the docs that are actually there: a checkout without
# .pi/ still has README.md and CONTRIBUTING.md to check, and gating the
# whole run on .pi/ would have made the human-facing half conditional on
# the agent-facing one.  This list is used twice — once to read the
# mentions out and once to say which file a missing target came from —
# because a second list is a second thing to forget.
# `ls` exits non-zero when *any* of its arguments is missing while still
# printing the ones that are there, and under `set -e` that would sink
# the assignment rather than scanning what exists, so swallow it.
docs="$({ find .pi -name '*.md' 2>/dev/null
	ls CLAUDE.md README.md CONTRIBUTING.md 2>/dev/null || true; })"

[ -n "$docs" ] || exit 0

# `make -pq` prints the database including every rule, without running
# anything.  It exits non-zero when a target is out of date, and under
# `pipefail` that would sink the whole assignment, so swallow it.
targets="$({ make -pqRr 2>/dev/null || true; } |
	awk '/^[a-zA-Z0-9][^$#\/\t=]*:([^=]|$)/ {sub(/:.*/, "", $0); print}' |
	sort -u)"

# A mention counts only when it is code: backticked (`make ui-test`)
# anywhere, or at the start of a line **inside a fenced block**.  Bare
# prose is not scanned, because English says things like "a renamed make
# target".
#
# The fence is why this is awk rather than one grep.  Line-start alone is
# not evidence of code in a file that is mostly hard-wrapped prose: the
# sentence "Two green branches do not / make a green merge" wrapped onto
# a line beginning `make a`, and the check duly failed on a target called
# `a`.  Inside a fence it is code; outside one it is a sentence that
# happened to break there, and a check that fails on reflow gets
# disabled rather than fixed.
#
# AGENTS.md is deliberately not in this list: it is a symlink to
# CLAUDE.md, asserted above, so scanning it would report every failure
# twice under two names.
mentioned="$(printf '%s\n' "$docs" |
	xargs awk '
		FNR == 1 { fence = 0 }
		/^```/   { fence = !fence; next }
		{
			rest = $0
			while (match(rest, /`make [a-z][a-z0-9-]*/)) {
				print substr(rest, RSTART + 6, RLENGTH - 6)
				rest = substr(rest, RSTART + RLENGTH)
			}
			if (fence && match($0, /^make [a-z][a-z0-9-]*/)) {
				print substr($0, 6, RLENGTH - 5)
			}
		}
	' | sort -u)"

missing=""

for t in $mentioned; do
	if ! printf '%s\n' "$targets" | grep -qx -- "$t"; then
		missing="$missing $t"
	fi
done

if [ -n "$missing" ]; then
	echo "skill-check: the docs name make targets that do not exist:" >&2
	for t in $missing; do
		echo "  make $t" >&2
		printf '%s\n' "$docs" | xargs grep -ln "make $t" | sed 's/^/      /' >&2
	done
	echo "Fix the docs, or restore the target." >&2
	exit 1
fi

echo "skill-check: $(printf '%s\n' "$mentioned" | wc -w) documented make targets, all present"
