#!/bin/sh
# Runs every test. Keyring and end-to-end tests use a throwaway keyring (a
# private gnome-keyring on Linux, temporary keychain files on macOS), so your
# real one is never touched.
set -eu
cd "$(dirname "$0")/.."

go vet ./...
go test -count=1 ./...

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin"
go build -o "$tmp/bin/" ./cmd/...
export QENV_TEST_KEYRING=1 QENV_TEST_KEYRING_PASSWORD=test

case "$(uname -s)" in
Linux)
	mkdir -p "$tmp/data" "$tmp/run"
	chmod 700 "$tmp/run"
	# GNOME_KEYRING_CONTROL would point gnome-keyring-daemon at your real keyring.
	env -u DISPLAY -u WAYLAND_DISPLAY -u GNOME_KEYRING_CONTROL \
		XDG_DATA_HOME="$tmp/data" XDG_RUNTIME_DIR="$tmp/run" \
		dbus-run-session -- sh -eu -c '
			printf %s "$QENV_TEST_KEYRING_PASSWORD" |
				gnome-keyring-daemon --unlock --components=secrets --foreground >/dev/null 2>&1 &
			trap "kill $! 2>/dev/null" EXIT
			# Wait for this daemon to own the name; calling the keyring before then would
			# make D-Bus start a second, locked one.
			i=0
			until busctl --user call org.freedesktop.DBus /org/freedesktop/DBus \
				org.freedesktop.DBus NameHasOwner s org.freedesktop.secrets 2>/dev/null | grep -q true; do
				i=$((i + 1))
				[ "$i" -lt 50 ] || { echo "test keyring did not start" >&2; exit 1; }
				sleep 0.1
			done
			go test -count=1 ./internal/keyring/
			python3 -I test/e2e.py "$1"
		' sh "$tmp/bin"
	;;
Darwin)
	trap 'for k in "$tmp"/*.keychain-db; do [ -e "$k" ] && security delete-keychain "$k"; done; rm -rf "$tmp"' EXIT
	# A fresh keychain per stage, so a test that locks one can't leave the next stage locked.
	use_keychain() {
		QENV_KEYCHAIN="$tmp/$1.keychain-db"
		export QENV_KEYCHAIN
		security create-keychain -p "$QENV_TEST_KEYRING_PASSWORD" "$QENV_KEYCHAIN"
		security set-keychain-settings "$QENV_KEYCHAIN" # never lock on its own
	}
	use_keychain go-tests
	go test -count=1 ./internal/keyring/
	use_keychain e2e
	python3 -I test/e2e.py "$tmp/bin"
	;;
*)
	echo "keyring and end-to-end tests only run on Linux and macOS" >&2
	exit 1
	;;
esac
