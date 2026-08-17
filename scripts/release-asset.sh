#!/usr/bin/env bash
#
# Attach a built artifact to the Gitea release for a tag.
#
# **It waits for the release to exist, and that is the point of the
# file.**  semantic-release pushes the tag in its `prepare` step and
# creates the release object in `publish` — so the tag push, which is
# what starts every publishing workflow, happens *before* there is a
# release id to upload to.  A fast publisher can therefore arrive first.
#
# The runner has capacity 1, which serialises things enough that this
# would usually work by accident; that is the worst kind of bug, so the
# wait is explicit and a timeout is a loud failure rather than a silently
# skipped asset.
#
# Usage:  scripts/release-asset.sh <tag> <file> [upload-name]
#
# Environment:
#   SERVER_URL      https://git.ljones.me
#   REPO            yonlu/yellowjacket
#   PACKAGE_TOKEN   a user PAT with write access
set -euo pipefail

tag="${1:?usage: release-asset.sh <tag> <file> [name]}"
file="${2:?usage: release-asset.sh <tag> <file> [name]}"
name="${3:-$(basename "$file")}"

: "${SERVER_URL:?SERVER_URL is not set}"
: "${REPO:?REPO is not set}"
: "${PACKAGE_TOKEN:?PACKAGE_TOKEN is not set}"

[ -s "$file" ] || { echo "release-asset: $file is missing or empty" >&2; exit 1; }

auth="Authorization: token ${PACKAGE_TOKEN}"
api="${SERVER_URL}/api/v1/repos/${REPO}"

# Up to five minutes.  A release that has not appeared by then means the
# release job failed, and this should say so rather than time out quietly.
release_id=""
for attempt in $(seq 1 60); do
	release_id=$(curl -sS -H "$auth" "${api}/releases/tags/${tag}" |
		jq -r 'if type == "object" and has("id") then .id else empty end')

	if [ -n "$release_id" ]; then
		echo "release-asset: release for $tag is id $release_id (after ${attempt} check(s))"
		break
	fi

	[ "$attempt" -eq 1 ] && echo "release-asset: waiting for the release for $tag to be created"
	sleep 5
done

if [ -z "$release_id" ]; then
	echo "release-asset: no release for $tag after 5 minutes." >&2
	echo "  The tag is pushed in semantic-release's prepare step and the release" >&2
	echo "  is created in publish, so this means the release job did not get that" >&2
	echo "  far.  Check the run of release.yml for this commit." >&2
	exit 1
fi

# Gitea refuses a duplicate asset name rather than replacing it, so a
# re-run of the same tag deletes the old one first.  That keeps a manual
# workflow_dispatch rebuild idempotent, which is the only reason anyone
# re-runs one of these.
existing=$(curl -sS -H "$auth" "${api}/releases/${release_id}/assets" |
	jq -r --arg n "$name" '.[]? | select(.name == $n) | .id')

if [ -n "$existing" ]; then
	echo "release-asset: replacing the existing '$name' (asset $existing)"
	curl -sS -o /dev/null -H "$auth" -X DELETE \
		"${api}/releases/${release_id}/assets/${existing}"
fi

echo "release-asset: uploading $name ($(du -h "$file" | cut -f1))"

code=$(curl -sS -o /tmp/release-asset.out -w '%{http_code}' \
	-H "$auth" \
	-X POST \
	-F "attachment=@${file};filename=${name}" \
	"${api}/releases/${release_id}/assets?name=${name}")

if [ "$code" != "201" ]; then
	echo "release-asset: upload returned $code" >&2
	cat /tmp/release-asset.out >&2
	exit 1
fi

echo "release-asset: attached $name to $tag"
