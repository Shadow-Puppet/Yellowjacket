#!/usr/bin/env bash
#
# Print the package id an APK actually declares — and, given --expect,
# refuse when that is not the id the caller was about to act on.
#
# This exists because the identity is declared twice and nothing made
# the two agree.  `applicationId` in build/android/app/build.gradle is
# what Gradle installs; `APP_ID` in build/android/Taskfile.yml was what
# every adb-driven task uninstalled, launched and filtered.  They differ
# for a reason nobody has to get wrong: the debug buildType carries
# `applicationIdSuffix ".dev"`, so a debug build is app.yellowjacket.dev
# while the default was app.yellowjacket — the *release* id, and on a
# real phone the released app with the user's library on it (#159).
#
# So the id is read back from the artifact rather than written down a
# third time.  The APK is the authority because the task that installs
# it has just built it: whatever Gradle resolved the applicationId to,
# suffixes and flavours included, is in the file, and no default can
# disagree with it.
#
# Usage:
#   android-pkgid.sh <apk> [--expect <id>]
#
# Exit codes: 0 printed the id; 1 could not read it; 2 --expect failed.
set -euo pipefail

die() { echo "android-pkgid: $*" >&2; exit 1; }

APK=""
EXPECT=""
while [ $# -gt 0 ]; do
	case "$1" in
	--expect) EXPECT="${2:-}"; shift 2 ;;
	-*) die "unknown option $1" ;;
	*) APK="$1"; shift ;;
	esac
done

[ -n "$APK" ] || die "usage: android-pkgid.sh <apk> [--expect <id>]"
[ -f "$APK" ] || die "no such APK: $APK"

# aapt2 lives under build-tools/<version>/, which is versioned, so it is
# resolved rather than pinned.  PATH first, so a system aapt2 (Arch ships
# one) works without an SDK layout at all.
find_aapt() {
	local sdk name
	for name in "$@"; do
		command -v "$name" 2>/dev/null && return 0
	done
	sdk="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-$HOME/Android/Sdk}}"
	for name in "$@"; do
		ls "$sdk"/build-tools/*/"$name" 2>/dev/null | sort -V | tail -1 | grep . && return 0
	done
	return 1
}

pkg=""

# `aapt2 dump packagename` answers in one word and is the cheapest of
# the three.  aapt1 is the fallback because it is what older build-tools
# carry and what the issue's own measurement used.
if AAPT2="$(find_aapt aapt2)"; then
	pkg="$("$AAPT2" dump packagename "$APK" 2>/dev/null | head -1 | tr -d '\r')" || true
fi

if [ -z "$pkg" ] && AAPT="$(find_aapt aapt)"; then
	pkg="$("$AAPT" dump badging "$APK" 2>/dev/null |
		sed -n "s/^package: name='\([^']*\)'.*/\1/p" | head -1)" || true
fi

# Guessing here is the bug this file exists to prevent, so an unreadable
# APK is a hard failure and never a fallback to a written-down default.
if [ -z "$pkg" ]; then
	die "could not read a package name from $APK.
    Install the SDK build-tools (aapt2), or set ANDROID_HOME to an SDK
    that carries them:  sdkmanager 'build-tools;34.0.0'"
fi

if [ -n "$EXPECT" ] && [ "$EXPECT" != "$pkg" ]; then
	cat >&2 <<EOF
android-pkgid: refusing to act on a package this APK does not declare.

    the APK declares:  $pkg
    the task expects:  $EXPECT
    APK:               $APK

These must agree, and when they do not it is the *expectation* that is
wrong: the APK is what Gradle built.  A debug build carries
applicationIdSuffix ".dev" (app/build.gradle), so a task that assembles
a debug APK and then addresses the unsuffixed id is addressing the
released app — which on a real device is the user's install, with their
library in it (#159).
EOF
	exit 2
fi

printf '%s\n' "$pkg"
