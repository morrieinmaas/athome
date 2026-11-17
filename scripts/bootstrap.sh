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
      [ $# -ge 2 ] && [ -n "$2" ] || die "--install-from needs a path"
      install_from="$2"
      shift 2
      ;;
    --install-from=*)
      install_from="${1#--install-from=}"
      [ -n "$install_from" ] || die "--install-from needs a path"
      shift
      ;;
    *)
      args+=("$1")
      shift
      ;;
  esac
done

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dest_dir="$HOME/.local/bin"
dest="$dest_dir/athome"
tmp=""
part=""
cleanup() {
  [ -z "$tmp" ] || rm -rf "$tmp"
  [ -z "$part" ] || rm -f "$part"
}
trap cleanup EXIT

install_file() {
  part="$dest_dir/.athome.$$"
  mkdir -p "$dest_dir" || die "cannot create $dest_dir"
  cp "$1" "$part" || { rm -f "$part"; die "cannot write $part"; }
  chmod 0755 "$part" || { rm -f "$part"; die "cannot make $part executable"; }
  mv -f "$part" "$dest" || { rm -f "$part"; die "cannot install $dest"; }
}

run_init() {
  # exec skips the EXIT trap, so clean up first.
  cleanup
  trap - EXIT
  export ATHOME_REPO="$repo_dir"
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

command -v curl >/dev/null 2>&1 || die "curl not found; install curl, or $hint"

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

# Only a genuinely missing asset (HTTP 404, or a missing file over file://)
# falls back; any other failure (network, 5xx) stops with curl's error.
url="$base/$tag/$asset"
rc=0
code="$(curl -fsSL -w '%{http_code}' "$url" -o "$tmp/$asset" 2>"$tmp/curl.err")" || rc=$?
if [ "$rc" -ne 0 ]; then
  missing=false
  [ "$code" = 404 ] && missing=true
  case "$base" in file://*) [ "$rc" -eq 37 ] && missing=true ;; esac
  $missing || die "cannot download $url: $(cat "$tmp/curl.err")"
  echo "bootstrap: no athome release for $tag; falling back to scripts/bootstrap-legacy.sh for this release" >&2
  cleanup
  trap - EXIT
  exec "$repo_dir/scripts/bootstrap-legacy.sh" ${args[@]+"${args[@]}"}
fi
curl -fsSL "$base/$tag/checksums.txt" -o "$tmp/checksums.txt" 2>"$tmp/curl.err" ||
  die "cannot download $base/$tag/checksums.txt: $(cat "$tmp/curl.err"); $hint"

want="$(awk -v a="$asset" '$2 == a || $2 == "*" a { print $1; exit }' "$tmp/checksums.txt")"
[ -n "$want" ] || die "checksums.txt has no line for $asset"
got="$(sha "$tmp/$asset")"
[ "$got" = "$want" ] || die "checksum mismatch for $asset: expected $want, got $got"

install_file "$tmp/$asset"
run_init
