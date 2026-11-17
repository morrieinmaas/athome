#!/usr/bin/env bash
# Installs a checksummed athome release to ~/.local/bin/athome, then runs `athome init`.
#
# Usage: scripts/bootstrap.sh [--install-from PATH] [athome init flags...]
#
#   --install-from PATH  install that file instead of downloading a release
#   --help               print this text
#
# Every other argument is passed to `athome init`.
# Environment: ATHOME_VERSION (release tag), ATHOME_RELEASE_BASE (download base URL).
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/bootstrap.sh [--install-from PATH] [athome init flags...]

Installs athome to ~/.local/bin/athome and runs `athome init` with the other flags.

  --install-from PATH  install that file instead of downloading a release
  --help               print this text

Environment:
  ATHOME_VERSION       release tag to install (default: the tag at HEAD, else the nearest tag)
  ATHOME_RELEASE_BASE  download base URL
                       (default: https://github.com/morrieinmaas/athome/releases/download)
USAGE
}

die() {
  echo "bootstrap: $*" >&2
  exit 1
}

install_from=""
args=()
while [ $# -gt 0 ]; do
  case "$1" in
    --help)
      usage
      exit 0
      ;;
    --install-from)
      [ $# -ge 2 ] || die "--install-from needs a path"
      install_from="$2"
      shift 2
      ;;
    *)
      args+=("$1")
      shift
      ;;
  esac
done

dest_dir="$HOME/.local/bin"
dest="$dest_dir/athome"
tmp=""
cleanup() { [ -z "$tmp" ] || rm -rf "$tmp"; }
trap cleanup EXIT

install_file() {
  mkdir -p "$dest_dir"
  local part="$dest_dir/.athome.$$"
  cp "$1" "$part" || { rm -f "$part"; die "cannot write $part"; }
  chmod 0755 "$part"
  mv -f "$part" "$dest" || { rm -f "$part"; die "cannot install $dest"; }
}

run_init() {
  exec "$dest" init ${args[@]+"${args[@]}"}
}

if [ -n "$install_from" ]; then
  [ -f "$install_from" ] || die "--install-from: no such file: $install_from"
  install_file "$install_from"
  run_init
fi

hint="build it with 'go build -o athome ./cmd/athome' and pass --install-from <path>"

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) die "unsupported os $(uname -s); $hint" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) die "unsupported arch $(uname -m); $hint" ;;
esac

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tag="${ATHOME_VERSION:-}"
if [ -z "$tag" ]; then
  tag="$(git -C "$repo_dir" describe --tags --exact-match HEAD 2>/dev/null ||
    git -C "$repo_dir" describe --tags --abbrev=0 2>/dev/null || true)"
fi
[ -n "$tag" ] || die "no release tag found; set ATHOME_VERSION, or $hint"

# Reuse an installed athome that already reports the wanted version.
if [ -x "$dest" ]; then
  have="$("$dest" version 2>/dev/null || true)"
  case "$have" in
    "athome $tag "* | "athome ${tag#v} "*)
      run_init
      ;;
  esac
fi

if command -v sha256sum >/dev/null 2>&1; then
  sha() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
  sha() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
  die "neither sha256sum nor shasum found; cannot verify the download"
fi

base="${ATHOME_RELEASE_BASE:-https://github.com/morrieinmaas/athome/releases/download}"
asset="athome_${os}_${arch}"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/athome-install.XXXXXX")"

curl -fsSL "$base/$tag/$asset" -o "$tmp/$asset" ||
  die "cannot download $base/$tag/$asset; $hint"
curl -fsSL "$base/$tag/checksums.txt" -o "$tmp/checksums.txt" ||
  die "cannot download $base/$tag/checksums.txt; $hint"

want="$(awk -v a="$asset" '$2 == a || $2 == "*" a { print $1; exit }' "$tmp/checksums.txt")"
[ -n "$want" ] || die "checksums.txt has no line for $asset"
got="$(sha "$tmp/$asset")"
[ "$got" = "$want" ] || die "checksum mismatch for $asset: expected $want, got $got"

install_file "$tmp/$asset"
run_init
