#!/bin/sh

set -eu

started_emulator=0
emulator_pid=""
emulator_serial=""
emulator_log=""

cleanup() {
	if [ "$started_emulator" -eq 1 ] && [ -n "$emulator_serial" ]; then
		adb -s "$emulator_serial" emu kill >/dev/null 2>&1 || true
	fi
	if [ -n "$emulator_pid" ]; then
		wait "$emulator_pid" 2>/dev/null || true
	fi
	if [ -n "$emulator_log" ]; then
		rm -f "$emulator_log"
	fi
}

trap cleanup EXIT INT TERM

running_emulator() {
	adb devices 2>/dev/null | awk '$1 ~ /^emulator-/ && $2 == "device" { print $1; exit }'
}

emulator_binary() {
	if [ -n "${ANDROID_HOME:-}" ] && [ -x "$ANDROID_HOME/emulator/emulator" ]; then
		printf '%s\n' "$ANDROID_HOME/emulator/emulator"
		return
	fi
	if [ -x "$HOME/Library/Android/sdk/emulator/emulator" ]; then
		printf '%s\n' "$HOME/Library/Android/sdk/emulator/emulator"
		return
	fi
	command -v emulator 2>/dev/null || true
}

if command -v adb >/dev/null 2>&1; then
	emulator_serial="$(running_emulator)"
	if [ -z "$emulator_serial" ]; then
		emulator="$(emulator_binary)"
		if [ -n "$emulator" ]; then
			avds="$($emulator -list-avds 2>/dev/null || true)"
			avd="${MUSIC_BRIDGE_ANDROID_TEST_AVD:-}"
			if [ -z "$avd" ]; then
				if printf '%s\n' "$avds" | grep -qx 'music_bridge_api35'; then
					avd="music_bridge_api35"
				else
					avd="$(printf '%s\n' "$avds" | sed -n '1p')"
				fi
			fi
			if [ -n "$avd" ]; then
				emulator_log="$(mktemp -t music-bridge-emulator)"
				"$emulator" "@$avd" -no-window -no-audio -no-snapshot-save -no-metrics >"$emulator_log" 2>&1 &
				emulator_pid=$!
				started_emulator=1
				attempt=0
				while [ "$attempt" -lt 60 ]; do
					emulator_serial="$(running_emulator)"
					[ -n "$emulator_serial" ] && break
					attempt=$((attempt + 1))
					sleep 1
				done
				if [ -z "$emulator_serial" ]; then
					printf 'Android emulator did not appear in adb devices. Log: %s\n' "$emulator_log" >&2
					exit 1
				fi
				attempt=0
				while [ "$attempt" -lt 90 ]; do
					booted="$(adb -s "$emulator_serial" shell getprop sys.boot_completed 2>/dev/null | tr -d '\r' || true)"
					[ "$booted" = "1" ] && break
					attempt=$((attempt + 1))
					sleep 1
				done
				if [ "${booted:-}" != "1" ]; then
					printf 'Android emulator did not finish booting. Log: %s\n' "$emulator_log" >&2
					exit 1
				fi
			fi
		fi
	fi
fi

go test -count=1 ./...
