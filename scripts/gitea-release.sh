#!/usr/bin/env bash
#
# Create the Gitea release for a version semantic-release has just tagged.
#
# This is `@semantic-release/exec`'s publishCmd, and it exists because
# Gitea's API is /api/v1 and @semantic-release/github speaks GitHub's.
# That is the whole of the Gitea-shaped work: one POST.
#
# **The notes come from CHANGELOG.md, not from an argument.**  Release
# notes are rendered commit messages — arbitrary text carrying backticks,
# quotes and `$` — so interpolating ${nextRelease.notes} into a shell
# command would be an injection whose input is the commit log.  The
# changelog plugin has already written them to the top of CHANGELOG.md by
# the time `publish` runs, so the only thing crossing the shell boundary
# here is a semver string, which is validated below anyway.
#
# Usage:  scripts/gitea-release.sh <version>      # e.g. 0.0.1
#
# Environment (all set by .gitea/workflows/release.yml):
#   SERVER_URL      https://git.ljones.me
#   OWNER           yonlu
#   REPO            yonlu/yellowjacket
#   PACKAGE_TOKEN   a user PAT with write access
set -euo pipefail

cd "$(dirname "$0")/.."

version="${1:?usage: gitea-release.sh <version>}"

# Validated rather than trusted: this is the one value that reaches a URL
# and a JSON document, and semantic-release is not the only thing that
# could ever call this.
if ! printf '%s' "$version" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'; then
	echo "gitea-release: '$version' is not a semver version" >&2
	exit 1
fi

: "${SERVER_URL:?SERVER_URL is not set}"
: "${REPO:?REPO is not set}"
: "${PACKAGE_TOKEN:?PACKAGE_TOKEN is not set}"

tag="v${version}"

# The top section of the changelog is this release's notes: everything
# from the first `## ` heading to the one after it.  awk rather than sed
# so the "there is no second heading" case (the first release) needs no
# special handling.
notes=$(awk '
	/^## / { seen++; if (seen > 1) exit }
	seen   { print }
' CHANGELOG.md)

if [ -z "$notes" ]; then
	echo "gitea-release: found no release section at the top of CHANGELOG.md" >&2
	echo '  the changelog plugin runs in prepare and this runs in publish, so' >&2
	echo '  an empty section means the plugin order in .releaserc.yml moved.' >&2
	exit 1
fi

echo "gitea-release: creating $tag from $(printf '%s' "$notes" | wc -l) lines of notes"

# jq builds the body, so a backtick or a quote in a commit subject is data
# rather than syntax.
payload=$(jq -n \
	--arg tag "$tag" \
	--arg name "$tag" \
	--arg body "$notes" \
	'{tag_name: $tag, name: $name, body: $body, draft: false, prerelease: false}')

code=$(curl -sS -o /tmp/gitea-release.out -w '%{http_code}' \
	-X POST \
	-H "Authorization: token ${PACKAGE_TOKEN}" \
	-H "Content-Type: application/json" \
	-d "$payload" \
	"${SERVER_URL}/api/v1/repos/${REPO}/releases")

case "$code" in
201)
	echo "gitea-release: created ${SERVER_URL}/${REPO}/releases/tag/${tag}"
	;;
409)
	# Already there.  The correct outcome for a re-run of the same tag,
	# and not a failure — the publish workflows are idempotent for the
	# same reason.
	echo "gitea-release: $tag already has a release; leaving it alone"
	;;
*)
	echo "gitea-release: POST /releases returned $code" >&2
	cat /tmp/gitea-release.out >&2
	exit 1
	;;
esac
