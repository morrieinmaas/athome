#!/usr/bin/env bash
# run.sh — build + run the athome bootstrap e2e locally (docker or podman).
#
#   test/e2e/run.sh          # build image + run the full bootstrap + asserts
#   test/e2e/run.sh --build  # build the image only
#   test/e2e/run.sh --clean  # remove leftover e2e containers + the image
#
# Passes a GITHUB_TOKEN into the run (from $GITHUB_TOKEN or `gh auth token`) so
# mise doesn't hit the anon rate limit. CI uses the same image via e2e.yml.
set -euo pipefail

cd "$(dirname "$0")/../.."   # repo root (Dockerfile COPYs the whole tree)

engine="$(command -v podman || command -v docker || true)"
[ -n "$engine" ] || { echo "need docker or podman on PATH" >&2; exit 1; }
img="athome-e2e:latest"

# Tear down e2e leftovers: force-remove any containers from the image (running or
# not — a killed local client can leave one Up inside the podman VM) then the
# image. Idempotent; safe to run anytime.
if [ "${1:-}" = "--clean" ]; then
  echo "==> removing e2e containers + image ($img)"
  ids="$("$engine" ps -aq --filter "ancestor=$img" 2>/dev/null || true)"
  [ -n "$ids" ] && "$engine" rm -f $ids >/dev/null 2>&1 || true
  "$engine" rmi -f "$img" >/dev/null 2>&1 || true
  echo "==> clean"
  exit 0
fi

# Arch Linux ships x86_64 only — pin amd64 so this works on Apple Silicon too
# (emulated, slow) and matches CI's amd64 runners (native).
platform="${ATHOME_E2E_PLATFORM:-linux/amd64}"

echo "==> building $img with ${engine##*/} (platform=$platform)"
"$engine" build --platform "$platform" -f test/e2e/Dockerfile -t "$img" .

[ "${1:-}" = "--build" ] && { echo "built (skip run)"; exit 0; }

token="${GITHUB_TOKEN:-$(gh auth token 2>/dev/null || true)}"
[ -n "$token" ] || echo "warning: no GITHUB_TOKEN / gh token — mise may hit the GitHub rate limit" >&2

# Under qemu emulation (the amd64 image on an arm64 host, e.g. Apple Silicon),
# AUR packages that build FROM SOURCE compile pathologically slowly — minutes
# each, looking hung. None are asserted by verify.sh, and CI installs them all
# natively, so for the local emulated run we skip them to keep it usable. On a
# native amd64 host (and in CI, which doesn't use this script) nothing is skipped
# and every package is installed. `-bin` packages stay — they're prebuilt + fast.
skip_env=()
host_arch="$(uname -m)"
if [ "$platform" = "linux/amd64" ] && [ "$host_arch" != "x86_64" ] && [ "$host_arch" != "amd64" ]; then
  # Keep the local emulated e2e usable by skipping packages that are slow to
  # build/download AND that verify.sh never asserts. CI runs natively and still
  # installs everything, so no coverage is lost. Three groups, trim freely:
  #   slow_aur     — compile from source → minutes each under qemu
  #   heavy_gui    — large prebuilt GUI binaries (editors/terminals/browsers)
  #   heavy_desktop — the GNOME/niri desktop stack + its big optional deps
  slow_aur="nirimod-git eternalterminal bandwhich gping trippy noctalia-shell podman-tui prettierd"
  heavy_gui="zed ghostty opencode-bin zen-browser-bin librewolf-bin"
  heavy_desktop="niri gnome-shell gnome-session gnome-control-center mutter nautilus gnome-shell-extensions evolution-data-server"
  skip_list="$slow_aur $heavy_gui $heavy_desktop"
  echo "==> emulated run ($host_arch host) — skipping heavy packages not asserted by verify.sh:"
  echo "    $skip_list"
  skip_env=(-e "ATHOME_SKIP_PACKAGES=$skip_list")
fi

echo "==> running bootstrap e2e"
# ${skip_env[@]+...} guards the empty-array expansion so it doesn't trip
# `set -u` on stock macOS bash 3.2 when no packages are skipped (native host).
exec "$engine" run --rm --platform "$platform" \
  -e "GITHUB_TOKEN=$token" -e "MISE_GITHUB_TOKEN=$token" \
  ${skip_env[@]+"${skip_env[@]}"} "$img"
