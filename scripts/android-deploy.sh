#!/usr/bin/env bash
#
# Install a built APK onto an Android target and launch it, under the
# package id the APK itself declares.
#
# This is the whole body of build/android/Taskfile.yml's four adb-driven
# tasks — deploy-emulator, run, run:device, deploy-device — which were
# three lines each, written out four times, and wrong in two ways in all
# four (#159):
#
#   adb uninstall app.yellowjacket    # the RELEASE id, unconditionally
#   adb install    bin/yellowjacket.apk
#   adb shell am start -n app.yellowjacket/com.wails.app.MainActivity
#
# **The uninstall is not here and does not come back.**  It was there to
# make the bare `install` on the next line work at all — without -r,
# Android refuses an install over an existing package — so `install -r`
# removes the reason for it rather than merely removing it.  What is
# left is the one case an uninstall really is the remedy, a changed
# signing certificate, and that is exactly the case where performing it
# silently costs the user their library.  So it is *named* and not done:
# an error message carrying the command is a decision the person at the
# keyboard gets to make, which is the same answer scripts/android-
# emulator.sh already reached for `make android-install`.
#
# **The id is read back from the artifact**, never defaulted, so the
# thing installed and the thing launched cannot disagree — see
# scripts/android-pkgid.sh for why that is by construction rather than
# by discipline.
#
# **The target is checked against the task's own name.**  The emulator
# tasks used a bare `adb`, which with one device attached picks that
# device whatever it is — so `wails3 task android:run`, whose summary
# says "in the Android Emulator", installed on the phone when a phone
# was the only thing plugged in.  A task addressing something other than
# what it says is the same fault as the package id, one level up.
#
# Usage:
#   android-deploy.sh --apk <path> --target emulator|device|any \
#                     [--expect <id>] [--serial <s>] [--no-launch]
set -euo pipefail

cd "$(dirname "$0")/.."

SDK="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-$HOME/Android/Sdk}}"
ADB="$(command -v adb || echo "$SDK/platform-tools/adb")"

# **Not "$PKG/.MainActivity".**  A leading-dot activity is resolved
# against the applicationId, and the scaffold's activity lives in the
# Java package com.wails.app, which is deliberately not it.  The short
# form fails with a class-not-found that reads like a broken build.
ACTIVITY="${YJ_ANDROID_ACTIVITY:-com.wails.app.MainActivity}"

APK=""
TARGET="any"
EXPECT=""
SERIAL="${ANDROID_SERIAL:-${DEVICE_ID:-}}"
LAUNCH=1

die() { echo "android-deploy: $*" >&2; exit 1; }

while [ $# -gt 0 ]; do
	case "$1" in
	--apk) APK="${2:-}"; shift 2 ;;
	--target) TARGET="${2:-}"; shift 2 ;;
	--expect) EXPECT="${2:-}"; shift 2 ;;
	--serial) SERIAL="${2:-}"; shift 2 ;;
	--no-launch) LAUNCH=0; shift ;;
	*) die "unknown option $1" ;;
	esac
done

[ -n "$APK" ] || die "--apk is required"
[ -f "$APK" ] || die "no such APK: $APK
    Build one first:  wails3 task android:assemble:apk   (debug)
                      wails3 task android:package        (release)"
[ -x "$ADB" ] || command -v adb >/dev/null ||
	die "adb not found. Install the Android SDK platform-tools (or set ANDROID_HOME)"

case "$TARGET" in
emulator | device | any) ;;
*) die "--target must be emulator, device or any (got '$TARGET')" ;;
esac

# ---------------------------------------------------------------- #
# Which package
# ---------------------------------------------------------------- #

# This runs *before* a target is chosen, deliberately: the guard is a
# question about the artifact, so it can be answered — and exercised —
# with nothing plugged in, and a build whose id is wrong should be
# refused whether or not there is anything to install it onto.
#
# An unreadable APK, or an id that is not the one the caller named, is a
# hard stop before anything is installed or launched.  Spelled as two
# calls rather than one with a conditional argument: an empty array under
# `set -u` is an unbound variable in bash 3.2, which is what macOS ships.
if [ -n "$EXPECT" ]; then
	PKG="$(./scripts/android-pkgid.sh "$APK" --expect "$EXPECT")"
else
	PKG="$(./scripts/android-pkgid.sh "$APK")"
fi

# ---------------------------------------------------------------- #
# Which target
# ---------------------------------------------------------------- #

# An emulator serial is "emulator-<port>"; anything else online is a
# physical device.  That is the same test the device tasks already made,
# and the emulator tasks did not make at all.
online_matching() {
	case "$TARGET" in
	emulator) "$ADB" devices | awk 'NR > 1 && $2 == "device" && $1 ~ /^emulator-/ { print $1 }' ;;
	device) "$ADB" devices | awk 'NR > 1 && $2 == "device" && $1 !~ /^emulator-/ { print $1 }' ;;
	any) "$ADB" devices | awk 'NR > 1 && $2 == "device" { print $1 }' ;;
	esac
}

if [ -z "$SERIAL" ]; then
	matches="$(online_matching)"
	count="$(printf '%s' "$matches" | grep -c . || true)"

	if [ "$count" -eq 0 ]; then
		echo "android-deploy: no ${TARGET/any/attached} target is online." >&2
		"$ADB" devices | sed '1d;/^$/d;s/^/    /' >&2 || true
		if [ "$TARGET" = "emulator" ]; then
			echo "    Start one with:  make android-emulator" >&2
		elif [ "$TARGET" = "device" ]; then
			echo "    Plug a phone in and authorise the adb key." >&2
		fi
		exit 1
	fi

	# Several is ambiguous, and picking the first silently is how a
	# build lands on a target nobody named.  The old run:device did
	# exactly that.
	if [ "$count" -gt 1 ]; then
		echo "android-deploy: several $TARGET targets are online — name one." >&2
		printf '%s\n' "$matches" | sed 's/^/    /' >&2
		echo "    Pass DEVICE_ID=<serial>, or set ANDROID_SERIAL." >&2
		exit 1
	fi

	SERIAL="$matches"
fi

# ---------------------------------------------------------------- #
# Install
# ---------------------------------------------------------------- #

echo "android-deploy: $APK ($PKG) -> $SERIAL"

if ! out="$("$ADB" -s "$SERIAL" install -r "$APK" 2>&1)"; then
	printf '%s\n' "$out"
	case "$out" in
	*INSTALL_FAILED_UPDATE_INCOMPATIBLE* | *"signatures do not match"*)
		cat >&2 <<EOF

The copy of $PKG already installed was signed with a different key, and
Android never allows that as an update.

The only way forward is an uninstall — **which deletes that app's data**,
and for this app that is the user's library, irreversibly.  So it is not
done for you.  If the installed copy is disposable:

    $ADB -s $SERIAL uninstall $PKG

If it is not — if this is a released build with a real library on it —
install the debug variant instead, which carries applicationIdSuffix
".dev" and so sits beside it rather than replacing it:

    wails3 task android:assemble:apk
EOF
		;;
	*INSTALL_FAILED_VERSION_DOWNGRADE*)
		cat >&2 <<EOF

The installed copy of $PKG has a higher versionCode than this build.
A bare 'make android' builds versionCode 1; a versioned one builds e.g.
10301.  Either build with a version:

    YJ_VERSION=1.3.1 YJ_VERSION_CODE=10301 make android

or, if the installed copy is disposable, remove it:

    $ADB -s $SERIAL uninstall $PKG
EOF
		;;
	esac
	exit 1
fi
printf '%s\n' "$out"

[ "$LAUNCH" -eq 1 ] || exit 0

"$ADB" -s "$SERIAL" shell am start -n "$PKG/$ACTIVITY"
