#!/usr/bin/env bash
# run.sh — build + run the athome bootstrap e2e locally (docker or podman).
#
#   test/e2e/run.sh          # build image + run the full bootstrap + asserts
#   test/e2e/run.sh --build  # build the image only
#   test/e2e/run.sh --clean  # remove leftover e2e containers + the image
#
# Distro is selected by ATHOME_E2E_DISTRO (default: arch); `fedora` is the other
# track. Each maps to its own Dockerfile + image tag so both can coexist:
#   ATHOME_E2E_DISTRO=fedora test/e2e/run.sh
#
# Passes a GITHUB_TOKEN into the run (from $GITHUB_TOKEN or `gh auth token`) so
# mise doesn't hit the anon rate limit. CI builds the same images via e2e.yml.
set -euo pipefail

cd "$(dirname "$0")/../.."   # repo root (the Dockerfile COPYs the whole tree)

engine="$(command -v podman || command -v docker || true)"
[ -n "$engine" ] || { echo "need docker or podman on PATH" >&2; exit 1; }

distro="${ATHOME_E2E_DISTRO:-arch}"
case "$distro" in
  arch)   dockerfile="test/e2e/Dockerfile" ;;
  fedora) dockerfile="test/e2e/Dockerfile.fedora" ;;
  *) echo "unknown ATHOME_E2E_DISTRO='$distro' (expected: arch | fedora)" >&2; exit 1 ;;
esac
img="athome-e2e-${distro}:latest"

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

echo "==> building $img ($distro) with ${engine##*/} (platform=$platform)"
"$engine" build --platform "$platform" -f "$dockerfile" -t "$img" .

[ "${1:-}" = "--build" ] && { echo "built (skip run)"; exit 0; }

token="${GITHUB_TOKEN:-$(gh auth token 2>/dev/null || true)}"
[ -n "$token" ] || echo "warning: no GITHUB_TOKEN / gh token — mise may hit the GitHub rate limit" >&2

# Under qemu emulation (the amd64 image on an arm64 host, e.g. Apple Silicon),
# heavy packages drag the run out — Arch AUR source-builds compile for minutes;
# big GUI/desktop binaries are slow downloads on either distro. None are asserted
# by verify.sh, and CI installs them all natively, so for the local emulated run
# we skip them to keep it usable. On a native amd64 host (and in CI, which builds
# the images directly, not via this script) nothing is skipped.
skip_env=()
host_arch="$(uname -m)"
if [ "$platform" = "linux/amd64" ] && [ "$host_arch" != "x86_64" ] && [ "$host_arch" != "amd64" ]; then
  case "$distro" in
    arch)
      # slow_aur compiles from source under qemu; heavy_gui/heavy_desktop are big
      # prebuilt downloads. Trim freely — names are Arch/AUR-specific.
      slow_aur="nirimod-git bandwhich gping trippy noctalia-shell podman-tui prettierd"
      heavy_gui="zed ghostty opencode-bin zen-browser-bin librewolf-bin"
      heavy_desktop="niri gdm gnome-shell gnome-session gnome-control-center mutter nautilus gnome-shell-extensions evolution-data-server"
      skip_list="$slow_aur $heavy_gui $heavy_desktop"
      ;;
    fedora)
      # Fedora has no source-build step (COPR ships prebuilt RPMs), so nothing
      # compiles — but the GUI/desktop RPMs are large. Skip the same not-asserted
      # heavy set, using Fedora names (from packages.yaml fedora.dnf / fedora.copr).
      heavy_gui="zed ghostty zen-browser"
      heavy_desktop="niri gdm noctalia-shell gnome-shell gnome-session gnome-control-center mutter nautilus gnome-extensions-app evolution-data-server"
      skip_list="$heavy_gui $heavy_desktop"
      ;;
  esac
  echo "==> emulated run ($host_arch host, $distro) — skipping heavy packages not asserted by verify.sh:"
  echo "    $skip_list"
  skip_env=(-e "ATHOME_SKIP_PACKAGES=$skip_list")
  # mise-side twin of ATHOME_SKIP_PACKAGES: rustc gets killed under qemu
  # ("rustc exited with non-zero status: no exit status"), which fails core:rust
  # and cascades to every cargo-backend tool — and run_after_06 now (rightly)
  # fails the apply on a broken mise install. None of these are asserted by
  # verify.sh. Bare registry names that resolve to the cargo backend (tokei)
  # are listed in both forms so the filter holds either way.
  mise_skip="rust,tokei,cargo:tokei,cargo:eza,cargo:just-lsp,cargo:navi,cargo:procs,cargo:tealdeer"
  echo "    (mise: $mise_skip)"
  skip_env+=(-e "MISE_DISABLE_TOOLS=$mise_skip")
  # Serialize mise installs under emulation: qemu-user's futex emulation can
  # deadlock mise's parallel extract/verify threads (observed: a mise child
  # futex-parked forever on gh-enhance with its download fd already deleted).
  # One job at a time removes the thread contention qemu trips over.
  skip_env+=(-e "MISE_JOBS=1")
fi

echo "==> running bootstrap e2e"
# ${skip_env[@]+...} guards the empty-array expansion so it doesn't trip
# `set -u` on stock macOS bash 3.2 when no packages are skipped (native host).
exec "$engine" run --rm --platform "$platform" \
  -e "GITHUB_TOKEN=$token" -e "MISE_GITHUB_TOKEN=$token" \
  ${skip_env[@]+"${skip_env[@]}"} "$img"
