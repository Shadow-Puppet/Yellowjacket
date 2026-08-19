#!/usr/bin/env bash
#
# Fails when `go generate ./...` would change something that is not staged.
#
# The obvious spelling of this is `go generate && git diff --name-only`,
# which is what the hook used to be, and it answers the wrong question:
# that diff is the *whole unstaged worktree*, so any unrelated edit — a
# note, a plan document, the next commit's files sitting there while this
# one lands — was reported as
#
#     Generated code is out of date. Run 'make generate' and stage the changes.
#
# Running `make generate` then does nothing, because nothing generated is
# stale, and the message sends you looking for a codegen problem that does
# not exist.  Splitting one piece of work into several commits is exactly
# the shape that triggers it, so the workaround was a constraint on commit
# order for no real reason.
#
# So the tree is snapshotted either side of the generators and only what
# *moved across them* is reported.  That is deliberately not a list of
# generated paths: sqlcgen, `*_templ.go` and `frontend/src/events.ts` are
# today's answer, a fourth generator is one `//go:generate` line away, and
# a path list is a second place to remember it — the same reasoning that
# keeps staleshape.go parsing sql/schemas/ rather than restating it.
#
# Content, not names: a generated file that is *already* dirty and is then
# rewritten further keeps its name in both snapshots and would otherwise
# slip through.

set -euo pipefail

cd "$(dirname "$0")/.."

# name + worktree blob hash for every file that differs from the index.
# A file listed but absent (a deletion) hashes as "gone" rather than
# aborting the pipeline.
snapshot() {
	git diff --name-only | while IFS= read -r f; do
		if [ -f "$f" ]; then
			printf '%s %s\n' "$f" "$(git hash-object -- "$f")"
		else
			printf '%s gone\n' "$f"
		fi
	done
}

# A brand-new generated file is not in either diff, because it is not
# tracked at all — the same blind spot bindings-check.sh names.  Both
# snapshots are taken before the generators run.
before="$(snapshot)"
before_untracked="$(git ls-files --others --exclude-standard)"

go generate ./...

after="$(snapshot)"
after_untracked="$(git ls-files --others --exclude-standard)"

# Symmetric difference, and the symmetry is the whole point.  Generation
# can push a file *into* the unstaged set (it was current, now it is not)
# or *out* of it (someone hand-edited generated output and the generator
# put it back) — and the second is stale generated code just as much as
# the first.  Comparing one direction only reports "current" for it,
# which is the failure this script was written to stop.
moved="$(comm -3 <(printf '%s\n' "$before" | sort) <(printf '%s\n' "$after" | sort) |
	cut -d' ' -f1 | tr -d '\t' | sort -u | grep -v '^$' || true)"

if [ -n "$moved" ]; then
	echo "codegen-check: generated code is out of date." >&2
	echo "Run 'make generate' and stage:" >&2
	printf '  %s\n' $moved >&2
	exit 1
fi

if [ "$after_untracked" != "$before_untracked" ]; then
	echo "codegen-check: generation produced new files. Stage them:" >&2
	comm -13 <(printf '%s\n' "$before_untracked" | sort) \
		<(printf '%s\n' "$after_untracked" | sort) >&2
	exit 1
fi

echo "codegen-check: generated code is current"
