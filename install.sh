#!/bin/sh
# canirun installer: curl -fsSL https://raw.githubusercontent.com/kavinbm16/canirun/main/install.sh | sh
#
# Downloads the release archive for your OS/CPU from GitHub, verifies its
# SHA-256 against checksums.txt, and installs the single binary.
# Environment:
#   CANIRUN_VERSION      version to install (default: latest release)
#   CANIRUN_INSTALL_DIR  where to put the binary (default: /usr/local/bin if writable, else ~/.local/bin)
#   CANIRUN_BASE_URL     release base URL (default: https://github.com/kavinbm16/canirun/releases)
set -eu

BASE="${CANIRUN_BASE_URL:-https://github.com/kavinbm16/canirun/releases}"

say() { printf '%s\n' "$*" >&2; }
die() { say "canirun install: $*"; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "needs '$1'"; }

need curl
need tar
need uname

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) die "unsupported OS $(uname -s); on Windows download the .zip from $BASE" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) die "unsupported CPU $(uname -m)" ;;
esac

version="${CANIRUN_VERSION:-}"
if [ -z "$version" ]; then
  # /releases/latest redirects to /releases/tag/vX.Y.Z
  url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$BASE/latest") || die "could not reach $BASE"
  version="${url##*/v}"
  [ -n "$version" ] && [ "$version" != "$url" ] || die "could not find the latest version"
fi
version="${version#v}"

file="canirun_${version}_${os}_${arch}.tar.gz"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

say "Downloading canirun $version for $os/$arch..."
curl -fsSL -o "$tmp/$file" "$BASE/download/v$version/$file" || die "download failed: $BASE/download/v$version/$file"
curl -fsSL -o "$tmp/checksums.txt" "$BASE/download/v$version/checksums.txt" || die "could not download checksums.txt"

want=$(awk -v f="$file" '$2 == f { print $1 }' "$tmp/checksums.txt")
[ -n "$want" ] || die "$file is not listed in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
  got=$(sha256sum "$tmp/$file" | awk '{print $1}')
else
  got=$(shasum -a 256 "$tmp/$file" | awk '{print $1}')
fi
[ "$want" = "$got" ] || die "checksum mismatch for $file (expected $want, got $got)"

tar -xzf "$tmp/$file" -C "$tmp" canirun

dir="${CANIRUN_INSTALL_DIR:-}"
if [ -z "$dir" ]; then
  if [ -w /usr/local/bin ]; then dir=/usr/local/bin; else dir="$HOME/.local/bin"; fi
fi
mkdir -p "$dir"
mv "$tmp/canirun" "$dir/canirun"
chmod 755 "$dir/canirun"

say "Installed $("$dir/canirun" version) to $dir/canirun"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) say "Add $dir to your PATH, e.g.: export PATH=\"$dir:\$PATH\"" ;;
esac
say "Try: canirun recommend    or    canirun ui"
