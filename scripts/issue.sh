#!/usr/bin/env bash
#
# The tracker, from the command line.
#
# Issues are this project's source of truth for what is wanted and what is
# already being worked on, which means "search the tracker" runs at the top
# of every task rather than occasionally.  Fifty-odd open issues make that a
# real lookup, and a lookup nobody can remember the shape of is a lookup that
# gets skipped — so it is one command here instead of a curl re-derived from
# prose each time.  See CLAUDE.md, "Issues".
#
# **Text reaches the API as JSON, never as shell.**  An issue body is
# arbitrary prose carrying backticks, quotes and `$`, so bodies are read from
# a file or from stdin and encoded by python3, on the same reasoning that
# keeps release notes out of `gitea-release.sh`'s argument list.  Only issue
# numbers and label names cross as arguments, and the numbers are validated.
#
# **Claiming is an assignment, a label and a comment, together.**  Any one of
# them alone is a claim somebody else has to go looking for: the assignee is
# what shows in the issue list, `Status/In Progress` is what filters, and the
# comment is what says which branch and what approach.  `claim` does all
# three, and refuses outright if somebody else already holds it.
#
# Usage:
#   scripts/issue.sh list [--state open|closed|all] [--label L] [--assignee U]
#   scripts/issue.sh mine
#   scripts/issue.sh search <text...>
#   scripts/issue.sh show <n>
#   scripts/issue.sh new --title <t> [--labels A,B] [--body-file F]
#   scripts/issue.sh claim <n> [--branch <name>] [--body-file F]
#   scripts/issue.sh unclaim <n>
#   scripts/issue.sh comment <n> [--body-file F]
#   scripts/issue.sh close <n> [--body-file F]
#   scripts/issue.sh label <n> +Kind/Bug -Status/Blocked
#   scripts/issue.sh depends <n> <blocker-n>
#   scripts/issue.sh labels
#
# Where a body is taken and no --body-file is given, it is read from stdin.
#
# Environment:
#   GITEA_TOKEN   a PAT with write:issue (plus write:repository and read:user,
#                 which the rest of this repo's tooling reaches for)
#   GITEA_URL     defaults to https://git.ljones.me
#   GITEA_REPO    defaults to yonlu/yellowjacket
set -euo pipefail

server="${GITEA_URL:-https://git.ljones.me}"
repo="${GITEA_REPO:-yonlu/yellowjacket}"
api="$server/api/v1/repos/$repo"

: "${GITEA_TOKEN:?issue.sh: GITEA_TOKEN is not set}"
command -v python3 >/dev/null || { echo "issue.sh: python3 is required" >&2; exit 1; }

py="$(dirname "$0")/issue_fmt.py"

# ---------------------------------------------------------------- plumbing

call() {
	local method="$1" path="$2"
	if [ "$method" = GET ]; then
		curl -sS -H "Authorization: token $GITEA_TOKEN" "$api$path"
	else
		curl -sS -X "$method" \
			-H "Authorization: token $GITEA_TOKEN" \
			-H "Content-Type: application/json" \
			--data-binary @- "$api$path"
	fi
}

num() {
	printf '%s' "${1:-}" | grep -qE '^[0-9]+$' || {
		echo "issue.sh: '${1:-}' is not an issue number" >&2
		exit 1
	}
	printf '%s' "$1"
}

# Read a body from a file or stdin.  A file of "-" is stdin.
read_body() {
	local file="${1:--}"
	if [ "$file" = "-" ]; then cat; else cat "$file"; fi
}

me() { curl -sS -H "Authorization: token $GITEA_TOKEN" "$server/api/v1/user" | python3 "$py" login; }

label_id() { call GET "/labels?limit=100" | python3 "$py" label-id "$1"; }

# Labels are resolved to ids rather than posted as names: Gitea accepts a list
# of unknown *names* with 200 and applies none of them, so a typo — or a label
# somebody renamed — reports success and does nothing.
add_labels() {
	local n="$1" ids
	shift
	ids="$(call GET "/labels?limit=100" | python3 "$py" label-ids "$(IFS=,; printf '%s' "$*")")"
	python3 "$py" add-label-ids "$ids" | call POST "/issues/$n/labels" |
		python3 "$py" check >/dev/null
}

drop_label() {
	local n="$1" name="$2" id
	id="$(label_id "$name" 2>/dev/null)" || return 0
	curl -sS -o /dev/null -X DELETE -H "Authorization: token $GITEA_TOKEN" \
		"$api/issues/$n/labels/$id"
}

post_comment() {
	local n="$1" text
	text="$(cat)"
	# Checked here rather than left to the API, which answers an empty body
	# with "[Body]: Required" and then this pipeline reports a second, more
	# confusing error from the request that was built anyway.
	if [ -z "${text//[[:space:]]/}" ]; then
		echo "issue.sh: refusing to post an empty comment on #$n" >&2
		exit 1
	fi
	printf '%s\n' "$text" | python3 "$py" wrap-body |
		call POST "/issues/$n/comments" | python3 "$py" check >/dev/null
}

# ---------------------------------------------------------------- commands

cmd_list() {
	local state=open label="" assignee="" limit=100 q=""
	while [ $# -gt 0 ]; do
		case "$1" in
		--state) state="$2"; shift 2 ;;
		--label) label="$2"; shift 2 ;;
		--assignee) assignee="$2"; shift 2 ;;
		--limit) limit="$2"; shift 2 ;;
		--q) q="$2"; shift 2 ;;
		*) echo "issue.sh list: unknown option $1" >&2; exit 1 ;;
		esac
	done

	local path="/issues?type=issues&state=$state&limit=$limit"
	[ -n "$label" ] && path="$path&labels=$(python3 "$py" urlquote "$label")"
	[ -n "$assignee" ] && path="$path&assigned_by=$assignee"
	[ -n "$q" ] && path="$path&q=$(python3 "$py" urlquote "$q")"

	call GET "$path" | python3 "$py" list
}

cmd_mine() { cmd_list --assignee "$(me)" "$@"; }

cmd_search() {
	[ $# -gt 0 ] || { echo "usage: issue.sh search <text...>" >&2; exit 1; }
	echo "-- open --"
	cmd_list --state open --q "$*"
	echo "-- closed --"
	cmd_list --state closed --q "$*"
}

cmd_show() {
	local n; n="$(num "${1:-}")"
	call GET "/issues/$n" | python3 "$py" show
	echo "-- depends on --"
	call GET "/issues/$n/dependencies" | python3 "$py" deps
	echo "-- comments --"
	call GET "/issues/$n/comments" | python3 "$py" comments
}

cmd_new() {
	local title="" labels="" file="-"
	while [ $# -gt 0 ]; do
		case "$1" in
		--title) title="$2"; shift 2 ;;
		--labels) labels="$2"; shift 2 ;;
		--body-file) file="$2"; shift 2 ;;
		*) echo "issue.sh new: unknown option $1" >&2; exit 1 ;;
		esac
	done
	[ -n "$title" ] || { echo "issue.sh new: --title is required" >&2; exit 1; }

	# Label names are resolved to ids first, so a typo is an error here rather
	# than an issue filed with a label silently absent.
	local ids="[]"
	if [ -n "$labels" ]; then
		ids="$(call GET "/labels?limit=100" | python3 "$py" label-ids "$labels")"
	fi

	read_body "$file" | python3 "$py" new-issue "$title" "$ids" |
		call POST "/issues" | python3 "$py" created
}

cmd_claim() {
	local n; n="$(num "${1:-}")"; shift || true
	local branch="" file=""
	while [ $# -gt 0 ]; do
		case "$1" in
		--branch) branch="$2"; shift 2 ;;
		--body-file) file="$2"; shift 2 ;;
		*) echo "issue.sh claim: unknown option $1" >&2; exit 1 ;;
		esac
	done

	local who holder note
	who="$(me)"
	holder="$(call GET "/issues/$n" | python3 "$py" assignees)"

	# The whole point of the workflow, so it is a hard failure.
	if [ -n "$holder" ] && [ "$holder" != "$who" ]; then
		echo "issue.sh: #$n is already claimed by $holder — talk to them before starting" >&2
		exit 1
	fi

	# The comment is resolved *before* anything is mutated.  Reading it after
	# the assignment is how a claim ends up half-made: the assignee and the
	# label land, the comment is rejected as empty, and the issue says it is
	# taken without saying by what work.
	if [ -n "$file" ]; then
		note="$(read_body "$file")"
	elif [ ! -t 0 ]; then
		note="$(read_body -)"
	fi
	if [ -z "${note//[[:space:]]/}" ]; then
		note="Starting work on this${branch:+ on \`$branch\`}."
	fi

	python3 "$py" assign "$who" | call PATCH "/issues/$n" | python3 "$py" check >/dev/null
	add_labels "$n" "Status/In Progress"
	printf '%s\n' "$note" | post_comment "$n"

	echo "claimed #$n as $who${branch:+ (branch $branch)}"
}

cmd_unclaim() {
	local n; n="$(num "${1:-}")"
	python3 "$py" assign | call PATCH "/issues/$n" | python3 "$py" check >/dev/null
	drop_label "$n" "Status/In Progress"
	echo "unclaimed #$n"
}

cmd_comment() {
	local n; n="$(num "${1:-}")"; shift || true
	local file="-"
	[ "${1:-}" = "--body-file" ] && file="$2"
	read_body "$file" | post_comment "$n"
	echo "commented on #$n"
}

cmd_close() {
	local n; n="$(num "${1:-}")"; shift || true
	local file=""
	[ "${1:-}" = "--body-file" ] && file="$2"
	if [ -n "$file" ]; then
		read_body "$file" | post_comment "$n"
	elif [ ! -t 0 ]; then
		read_body - | post_comment "$n"
	fi
	printf '{"state":"closed"}' | call PATCH "/issues/$n" | python3 "$py" check >/dev/null
	# A claim outlives the work if nothing takes the label off.
	drop_label "$n" "Status/In Progress"
	echo "closed #$n"
}

# Hard blockers are real Gitea dependencies, which render on the issue itself
# — see #73, whose graph is the reason this is not just prose in a comment.
#
# The endpoint takes a whole IssueMeta, not an index: a body of {"index": 88}
# answers **404**, which reads exactly like a missing endpoint on a Gitea
# build that does not have the feature.
cmd_depends() {
	local n blocker
	n="$(num "${1:-}")"
	blocker="$(num "${2:-}")"
	python3 "$py" issue-meta "$repo" "$blocker" |
		call POST "/issues/$n/dependencies" | python3 "$py" check >/dev/null
	echo "#$n now depends on #$blocker"
}

cmd_label() {
	local n; n="$(num "${1:-}")"; shift
	local add=() del=()
	for spec in "$@"; do
		case "$spec" in
		+*) add+=("${spec#+}") ;;
		-*) del+=("${spec#-}") ;;
		*) echo "issue.sh label: expected +Name or -Name, got '$spec'" >&2; exit 1 ;;
		esac
	done
	if [ ${#add[@]} -gt 0 ]; then
		add_labels "$n" "${add[@]}"
	fi
	local name
	for name in ${del[@]+"${del[@]}"}; do
		drop_label "$n" "$name"
	done
	echo "relabelled #$n"
}

# ---------------------------------------------------------------- dispatch

sub="${1:-}"
[ $# -gt 0 ] && shift

case "$sub" in
list) cmd_list "$@" ;;
mine) cmd_mine "$@" ;;
search) cmd_search "$@" ;;
show) cmd_show "$@" ;;
new) cmd_new "$@" ;;
claim) cmd_claim "$@" ;;
unclaim) cmd_unclaim "$@" ;;
comment) cmd_comment "$@" ;;
close) cmd_close "$@" ;;
label) cmd_label "$@" ;;
depends) cmd_depends "$@" ;;
labels) call GET "/labels?limit=100" | python3 "$py" labels ;;
*)
	sed -n '/^# Usage:/,/^# Environment:/p' "$0" | sed 's/^# \{0,1\}//'
	exit 1
	;;
esac
