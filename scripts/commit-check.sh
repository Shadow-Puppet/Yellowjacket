#!/usr/bin/env bash
#
# Conventional Commits, enforced.  CLAUDE.md has claimed for a long time
# that commitlint gates this in CI; there was no config and no workflow
# running one, so the claim was a lie in the file every contributor and
# every agent reads first.  This is the smaller of the two honest
# answers to that: the grammar is twenty lines of shell, and adding
# commitlint would mean a Node dependency tree at the root of a Go repo
# purely to regex one line.
#
# The type list is the one .releaserc.yml's commit-analyzer knows about
# — keep the two in step, or semantic-release will silently decline to
# release something this accepts.
#
# Usage:
#   scripts/commit-check.sh                  lint HEAD's message
#   scripts/commit-check.sh <file>           lint a message file (commit-msg hook)
#   scripts/commit-check.sh --range A..B     lint every commit in a range
set -euo pipefail

cd "$(dirname "$0")/.."

TYPES='build|chore|ci|docs|feat|fix|perf|refactor|revert|style|test'
MAX_SUBJECT=72

# A subject is `type(optional-scope)!: text`.  The `!` is Conventional
# Commits' breaking-change marker and is allowed with or without a scope.
SUBJECT_RE="^(${TYPES})(\([a-z0-9._/-]+\))?!?: .+$"

fail=0

check_subject() {
	local subject="$1" label="$2"

	# Exemptions, all of them things git or a tool writes rather than a
	# person: merges, reverts of a revert, and the autosquash markers,
	# which carry the *original* subject and are rewritten by the rebase
	# that consumes them.
	case "$subject" in
	Merge\ * | Revert\ * | fixup!* | squash!* | amend!*) return 0 ;;
	esac

	if ! printf '%s' "$subject" | grep -qE "$SUBJECT_RE"; then
		echo "commit-check: $label" >&2
		echo "    $subject" >&2
		echo "  is not 'type(scope): subject'." >&2
		echo "  Types: $(printf '%s' "$TYPES" | tr '|' ' ')" >&2
		fail=1

		return 0
	fi

	if [ "${#subject}" -gt "$MAX_SUBJECT" ]; then
		echo "commit-check: $label" >&2
		echo "    $subject" >&2
		echo "  subject is ${#subject} chars; the limit is ${MAX_SUBJECT}." >&2
		fail=1
	fi

	# godot for commits: the Go side of this repo lints comments for a
	# trailing period, and a subject is the one line that must not have
	# one.
	case "$subject" in
	*.)
		echo "commit-check: $label" >&2
		echo "    $subject" >&2
		echo "  subject ends with a period." >&2
		fail=1
		;;
	esac
}

case "${1:-}" in
--range)
	range="${2:?--range needs A..B}"
	count=0
	while IFS= read -r line; do
		[ -n "$line" ] || continue
		sha="${line%% *}"
		check_subject "${line#* }" "$sha"
		count=$((count + 1))
	done <<<"$(git log --no-merges --format='%H %s' "$range")"
	[ "$fail" -eq 0 ] && echo "commit-check: $count commits in $range, all well-formed"
	;;
"")
	check_subject "$(git log -1 --format='%s')" "HEAD"
	[ "$fail" -eq 0 ] && echo "commit-check: HEAD is well-formed"
	;;
*)
	# The commit-msg hook hands us a file that also contains the body and
	# git's own comment lines; the subject is its first line.
	check_subject "$(head -n 1 "$1")" "$1"
	[ "$fail" -eq 0 ] && echo "commit-check: message is well-formed"
	;;
esac

exit "$fail"
