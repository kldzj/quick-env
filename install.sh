#!/bin/sh
# Installs qe and qenv from a GitHub release, after checking the archive's SHA-256.
#
#   curl -fsSL https://raw.githubusercontent.com/kldzj/quick-env/main/install.sh | sh
#
# Settings, all optional:
#   INSTALL_DIR  where the binaries go (default: ~/.local/bin, no sudo needed)
#   VERSION      a release tag such as v0.1.0 (default: the latest release)
#   QUICK_ENV_RELEASES  base URL of the releases, for mirrors
#                       (default: https://github.com/kldzj/quick-env/releases)
set -eu

install_dir=${INSTALL_DIR:-$HOME/.local/bin}
version=${VERSION:-latest}
releases=${QUICK_ENV_RELEASES:-https://github.com/kldzj/quick-env/releases}

say() { printf '%s\n' "$*" >&2; }
die() {
	say "install.sh: $*"
	exit 1
}

for tool in curl tar uname mktemp awk; do
	command -v "$tool" >/dev/null 2>&1 || die "needs $tool"
done
if command -v sha256sum >/dev/null 2>&1; then
	sha256() { sha256sum "$1" | awk '{ print $1 }'; }
elif command -v shasum >/dev/null 2>&1; then
	sha256() { shasum -a 256 "$1" | awk '{ print $1 }'; }
else
	die "needs sha256sum or shasum to check the download"
fi

case "$(uname -s)" in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) die "qe and qenv run on Linux and macOS, not $(uname -s)" ;;
esac
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) die "no build for this CPU: $(uname -m)" ;;
esac
# An Apple Silicon Mac running this under Rosetta still wants the native build.
if [ "$os" = darwin ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || true)" = 1 ]; then
	arch=arm64
fi

if [ "$version" = latest ]; then
	# /releases/latest redirects to /releases/tag/<version>.
	url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$releases/latest") ||
		die "couldn't find the latest release at $releases"
	version=${url##*/}
	[ -n "$version" ] && [ "$version" != latest ] || die "couldn't find the latest release at $releases"
fi

archive="quick-env_${version#v}_${os}_${arch}.tar.gz"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"; rm -f "$install_dir/.qe.new" "$install_dir/.qenv.new"' EXIT

say "downloading $archive"
curl -fsSL -o "$tmp/$archive" "$releases/download/$version/$archive" ||
	die "download failed: $releases/download/$version/$archive"
curl -fsSL -o "$tmp/checksums.txt" "$releases/download/$version/checksums.txt" ||
	die "download failed: $releases/download/$version/checksums.txt"

expected=$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")
[ -n "$expected" ] || die "checksums.txt has no entry for $archive"
[ "$(sha256 "$tmp/$archive")" = "$expected" ] || die "checksum mismatch for $archive; nothing was installed"

tar xzf "$tmp/$archive" -C "$tmp" qe qenv
mkdir -p "$install_dir"
for bin in qe qenv; do
	# Copy then rename, so a running qe or qenv is replaced cleanly.
	cp "$tmp/$bin" "$install_dir/.$bin.new"
	chmod 755 "$install_dir/.$bin.new"
	mv -f "$install_dir/.$bin.new" "$install_dir/$bin"
done
"$install_dir/qe" -h >/dev/null 2>&1 || die "installed qe doesn't run"

say "installed qe and qenv $version to $install_dir"
case ":$PATH:" in
*":$install_dir:"*) ;;
*) say "note: $install_dir isn't on your PATH yet; add it with: export PATH=\"$install_dir:\$PATH\"" ;;
esac
