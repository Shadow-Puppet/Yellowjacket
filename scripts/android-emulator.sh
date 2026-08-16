#!/usr/bin/env bash
#
# The Android tier: an emulator, an APK, and a way to find out why the
# app died.
#
# This is the phone equivalent of `dev-headless.sh`, and it is
# deliberately shaped like it — start in the background and return,
# stop by saved state, tail a log — because the operating pattern is
# the one this repo already has. What is different is *what a failure
# looks like*, and that is the whole reason this script exists rather
# than a paragraph telling you to run adb.
#
# **Go's stdout does not reach logcat.** An Android app's fd 1 and 2 go
# to /dev/null, so every `slog` line the app writes — including the one
# naming the error it is about to exit on — is discarded. There is no
# flag for this: `setprop log.redirect-stdio true` redirects the *Java*
# runtime's System.out and does nothing for a c-shared Go library.
#
# **And `os.Exit` is a silent death.** `main()` ends several failure
# paths in `os.Exit(1)`; from Android's side that is a process that
# vanished, reported as "has died: fg TOP" and signal 9, with no panic,
# no `AndroidRuntime` stack and no tombstone — the three places anyone
# would look. ActivityManager then restarts it, so `pidof` answers with
# a pid and the app looks alive while crash-looping several times a
# second.
#
# `smoke` exists because of those two facts together: the honest test
# is not "did it start" but "is the same pid still there a few seconds
# later", and the useful output is the app's own logcat tags plus a
# named guess at which `os.Exit` it took.
set -euo pipefail

cd "$(dirname "$0")/.."

AVD="${YJ_AVD:-yj-test}"
SDK="${ANDROID_SDK_ROOT:-${ANDROID_HOME:-$HOME/Android/Sdk}}"
PKG="${YJ_ANDROID_PKG:-app.yellowjacket}"
# **Not "$PKG/.MainActivity".** A leading-dot activity is resolved
# relative to the *applicationId*, and the scaffold's activity lives in
# the Java package `com.wails.app`, which is deliberately not the
# applicationId (see app/build.gradle). The short form silently
# resolves to app.yellowjacket.MainActivity, which does not exist, and
# `am start` fails with a class-not-found that reads like a broken
# build rather than a wrong name.
ACTIVITY="${YJ_ANDROID_ACTIVITY:-com.wails.app.MainActivity}"
IMAGE="${YJ_ANDROID_IMAGE:-system-images;android-35;google_apis;x86_64}"
DEVDIR=".dev"
PIDFILE="$DEVDIR/emulator.pid"
LOGFILE="$DEVDIR/emulator.log"

ADB="$SDK/platform-tools/adb"
EMULATOR="$SDK/emulator/emulator"
SDKMANAGER="$SDK/cmdline-tools/latest/bin/sdkmanager"
AVDMANAGER="$SDK/cmdline-tools/latest/bin/avdmanager"

die() { echo "android: $*" >&2; exit 1; }

need_sdk() {
	[ -x "$ADB" ] || die "no adb at $ADB — set ANDROID_SDK_ROOT, or run 'make android-setup'"
	[ -x "$EMULATOR" ] || die "no emulator at $EMULATOR — run 'make android-setup'"
}

# The emulator is the only long-lived process here, and it is addressed
# by its saved pid.  Never by name: `pkill -f emulator` matches this
# script's own command line and kills the shell running it, which is
# the same trap dev-stop.sh documents.
running() {
	[ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null
}

cmd_setup() {
	[ -x "$SDKMANAGER" ] || die "no sdkmanager at $SDKMANAGER; install the Android command line tools first"

	# Each piece is installed only when missing.  sdkmanager is itself
	# idempotent but still spends minutes verifying, so the guards are
	# what make this cheap to re-run.
	for want in "platform-tools" "platforms;android-35" "build-tools;34.0.0" "$IMAGE"; do
		dir="$SDK/$(printf '%s' "$want" | tr ';' '/')"
		if [ -d "$dir" ]; then
			echo "  $want: present"
		else
			echo "  $want: installing"
			yes | "$SDKMANAGER" --install "$want" >/dev/null
		fi
	done

	if "$EMULATOR" -list-avds 2>/dev/null | grep -qx "$AVD"; then
		echo "  avd $AVD: present"
	else
		echo "  avd $AVD: creating"
		echo no | "$AVDMANAGER" create avd -n "$AVD" -k "$IMAGE" -d pixel_6 --force >/dev/null
	fi

	# A dependency with a requirement, checked like one.  Without KVM the
	# emulator falls back to full software emulation and a boot that
	# takes 30 s takes 20 minutes — which reads as a hung target.
	if ! "$EMULATOR" -accel-check 2>&1 | grep -q "is installed and usable"; then
		echo
		echo "  WARNING: KVM is not usable.  The emulator will run under software"
		echo "  emulation and boot times go from ~30s to tens of minutes."
		echo "  Check /dev/kvm exists and that you are in the kvm group."
	fi
}

cmd_start() {
	need_sdk
	mkdir -p "$DEVDIR"

	if running; then
		echo "emulator already running (pid $(cat "$PIDFILE"))"
	else
		"$EMULATOR" -list-avds 2>/dev/null | grep -qx "$AVD" ||
			die "no AVD named '$AVD' — run 'make android-setup'"

		# -no-window because there is no display and does not need one;
		# -no-snapshot so a run starts from the same state every time,
		# which is what makes a smoke result mean something.
		nohup "$EMULATOR" -avd "$AVD" \
			-no-window -no-boot-anim -no-snapshot \
			-gpu swiftshader_indirect \
			-netdelay none -netspeed full \
			>"$LOGFILE" 2>&1 &
		echo $! >"$PIDFILE"
		echo "emulator starting (pid $(cat "$PIDFILE")), log: $LOGFILE"
	fi

	echo -n "waiting for boot"
	"$ADB" wait-for-device >/dev/null 2>&1 || die "device never appeared; see $LOGFILE"
	for _ in $(seq 1 150); do
		if [ "$("$ADB" shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" = "1" ]; then
			echo " ok"
			"$ADB" shell getprop ro.build.version.release |
				sed 's/^/  android /'
			return 0
		fi
		echo -n .
		sleep 2
	done
	echo
	die "boot did not complete in 300s; see $LOGFILE"
}

cmd_stop() {
	if running; then
		pid=$(cat "$PIDFILE")
		# The emulator's own console command shuts the guest down
		# cleanly; the saved pid is the fallback and the guarantee.
		"$ADB" emu kill >/dev/null 2>&1 || true
		for _ in $(seq 1 15); do
			kill -0 "$pid" 2>/dev/null || break
			sleep 1
		done
		kill -0 "$pid" 2>/dev/null && kill "$pid" 2>/dev/null || true
		echo "emulator stopped"
	else
		echo "emulator not running"
	fi
	rm -f "$PIDFILE"
}

cmd_install() {
	need_sdk
	[ -f bin/yellowjacket.apk ] || die "no bin/yellowjacket.apk — run 'make android' first"
	"$ADB" get-state >/dev/null 2>&1 || die "no device — run 'make android-emulator' first"
	"$ADB" install -r bin/yellowjacket.apk
}

cmd_launch() {
	need_sdk
	"$ADB" shell am force-stop "$PKG"
	"$ADB" logcat -c
	"$ADB" shell am start -n "$PKG/$ACTIVITY" >/dev/null
}

cmd_logs() {
	need_sdk
	# The app's own tags plus the two that report its death.  Chasing a
	# raw logcat here is hopeless: the emulator emits thousands of lines
	# a second, almost all of them WindowManager transitions.
	"$ADB" logcat -v time \
		WailsBridge:V "$PKG":V GoLog:V AndroidRuntime:E DEBUG:V libc:F ActivityManager:I '*:S'
}

# Start the app and assert it is *still the same process* a few seconds
# later.  "It started" is not the question — a crash-looping app starts
# continuously.
cmd_smoke() {
	need_sdk
	local wait_s="${1:-10}"

	cmd_launch
	sleep 3
	local first
	first=$("$ADB" shell pidof "$PKG" 2>/dev/null | tr -d '\r' | awk '{print $1}')
	sleep "$wait_s"
	local second
	second=$("$ADB" shell pidof "$PKG" 2>/dev/null | tr -d '\r' | awk '{print $1}')

	if [ -n "$first" ] && [ "$first" = "$second" ]; then
		echo "PASS: $PKG alive as pid $first after ${wait_s}s"
		return 0
	fi

	echo "FAIL: $PKG is not stable (pid was '${first:-none}', now '${second:-none}')"
	if [ -n "$first" ] && [ -n "$second" ]; then
		echo "      The pid changed: it is crash-looping, not running."
	fi
	echo
	echo "--- last 40 app-relevant logcat lines ---"
	"$ADB" logcat -d -v time \
		WailsBridge:V "$PKG":V GoLog:V AndroidRuntime:E DEBUG:V libc:F '*:S' 2>/dev/null |
		tail -40
	echo
	echo "--- reading this ---"
	echo "If the last line is 'Wails bridge initialized' and nothing follows,"
	echo "the Go side reached main() and left it.  There will be no panic and"
	echo "no tombstone, because that is os.Exit, not a crash.  Go's stdout"
	echo "does not reach logcat, so the slog line naming the error is gone."
	echo "Work backwards through main()'s os.Exit(1) paths instead."
	return 1
}

case "${1:-}" in
setup) cmd_setup ;;
start) cmd_start ;;
stop) cmd_stop ;;
install) cmd_install ;;
launch) cmd_launch ;;
logs) cmd_logs ;;
smoke) cmd_smoke "${2:-10}" ;;
*)
	echo "usage: $0 {setup|start|stop|install|launch|logs|smoke [seconds]}" >&2
	exit 2
	;;
esac
