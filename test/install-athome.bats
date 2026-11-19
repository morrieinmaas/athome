#!/usr/bin/env bats
# Tests for home/.chezmoiscripts/run_14-install-athome.sh.tmpl. The
# template is rendered against home/.chezmoidata/pins.toml, then run with a
# fake HOME and a file:// release base. No network.

bats_require_minimum_version 1.5.0

setup() {
  unset TMUX GROFE ATHOME_CI
  ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  command -v chezmoi >/dev/null || skip "chezmoi not installed"
  export TMPDIR="$BATS_TEST_TMPDIR/tmp"
  FAKEHOME="$BATS_TEST_TMPDIR/fh"
  DEST="$FAKEHOME/.local/bin/athome"
  mkdir -p "$TMPDIR" "$FAKEHOME"
  : >"$BATS_TEST_TMPDIR/empty.toml"
  TMPL="$ROOT/home/.chezmoiscripts/run_14-install-athome.sh.tmpl"
  SCRIPT="$BATS_TEST_TMPDIR/install.sh"
  chezmoi --source "$ROOT/home" --config "$BATS_TEST_TMPDIR/empty.toml" execute-template <"$TMPL" >"$SCRIPT"
  PIN="$(awk -F'"' '/^athome/ { print $2 }' "$ROOT/home/.chezmoidata/pins.toml")"
  case "$(uname -s)" in Darwin) os=darwin ;; *) os=linux ;; esac
  case "$(uname -m)" in x86_64 | amd64) arch=amd64 ;; *) arch=arm64 ;; esac
  ASSET="athome_${os}_${arch}"
  REL="$BATS_TEST_TMPDIR/rel"
  mkdir -p "$REL/$PIN"
  fake_athome "$REL/$PIN/$ASSET" "${PIN#v}"
  (cd "$REL/$PIN" && shasum -a 256 "$ASSET" >checksums.txt)
  export ATHOME_RELEASE_BASE="file://$REL"
}

# fake_athome <path> <version>: a script that answers `athome version`.
fake_athome() {
  mkdir -p "$(dirname "$1")"
  printf '#!/bin/sh\necho "athome %s (abc1234)"\n' "$2" >"$1"
  chmod 0755 "$1"
}

install_athome() {
  HOME="$FAKEHOME" run bash "$SCRIPT"
}

@test "a missing athome is installed at the pin, mode 0755, with no temp files left" {
  install_athome
  [ "$status" -eq 0 ]
  [[ "$output" == *"athome: installed $PIN at $DEST"* ]]
  [ "$("$DEST" version)" = "athome ${PIN#v} (abc1234)" ]
  [ "$(stat -c %a "$DEST" 2>/dev/null || stat -f %Lp "$DEST")" = 755 ]
  [ -z "$(ls -A "$TMPDIR")" ]
  [ "$(ls -A "$FAKEHOME/.local/bin")" = athome ]
}

@test "an athome at the pin is left alone and nothing is downloaded" {
  fake_athome "$DEST" "${PIN#v}"
  echo "# mine" >>"$DEST"
  rm -rf "$REL"
  install_athome
  [ "$status" -eq 0 ]
  [ -z "$output" ]
  grep -q '^# mine$' "$DEST"
}

@test "a newer athome is never downgraded" {
  fake_athome "$DEST" 99.0.0
  install_athome
  [ "$status" -eq 0 ]
  [ "$("$DEST" version)" = "athome 99.0.0 (abc1234)" ]
}

@test "versions compare numerically, not as text" {
  # v0.10.0 is above v0.9.0, so a pin of v0.9.0 keeps 0.10.0.
  src="$BATS_TEST_TMPDIR/src"
  cp -R "$ROOT/home" "$src"
  perl -pi -e 's/^athome = ".*"/athome = "v0.9.0"/' "$src/.chezmoidata/pins.toml"
  chezmoi --source "$src" --config "$BATS_TEST_TMPDIR/empty.toml" execute-template <"$TMPL" >"$SCRIPT"
  fake_athome "$DEST" 0.10.0
  install_athome
  [ "$status" -eq 0 ]
  [ "$("$DEST" version)" = "athome 0.10.0 (abc1234)" ]
}

@test "an older athome is replaced" {
  fake_athome "$DEST" 0.0.1
  install_athome
  [ "$status" -eq 0 ]
  [ "$("$DEST" version)" = "athome ${PIN#v} (abc1234)" ]
}

@test "a development build is left alone" {
  fake_athome "$DEST" dev
  install_athome
  [ "$status" -eq 0 ]
  [ -z "$output" ]
  [ "$("$DEST" version)" = "athome dev (abc1234)" ]
}

@test "an athome whose version command fails is replaced" {
  printf '#!/bin/sh\nexit 3\n' >"$BATS_TEST_TMPDIR/broken"
  mkdir -p "$(dirname "$DEST")"
  install -m 0755 "$BATS_TEST_TMPDIR/broken" "$DEST"
  install_athome
  [ "$status" -eq 0 ]
  [ "$("$DEST" version)" = "athome ${PIN#v} (abc1234)" ]
}

@test "a checksum mismatch exits 1 and keeps the old binary" {
  fake_athome "$DEST" 0.0.1
  echo "$(printf '0%.0s' $(seq 64))  $ASSET" >"$REL/$PIN/checksums.txt"
  install_athome
  [ "$status" -eq 1 ]
  [[ "$output" == *"checksum mismatch for $ASSET"* ]]
  [ "$("$DEST" version)" = "athome 0.0.1 (abc1234)" ]
  [ "$(ls -A "$FAKEHOME/.local/bin")" = athome ]
  [ -z "$(ls -A "$TMPDIR")" ]
}

@test "checksums.txt without a line for the asset exits 1 and installs nothing" {
  echo "$(printf '0%.0s' $(seq 64))  athome_plan9_386" >"$REL/$PIN/checksums.txt"
  install_athome
  [ "$status" -eq 1 ]
  [[ "$output" == *"expected nothing"* ]]
  [ ! -e "$DEST" ]
}

@test "a failed download exits 0 with a note and installs nothing" {
  rm "$REL/$PIN/$ASSET"
  install_athome
  [ "$status" -eq 0 ]
  [[ "$output" == *"athome: cannot download file://$REL/$PIN/$ASSET ("* ]]
  [[ "$output" == *"), so athome $PIN is not installed; the next apply tries again"* ]]
  [ ! -e "$DEST" ]
  [ -z "$(ls -A "$TMPDIR")" ]
}

@test "an unreachable checksums.txt exits 0 and installs nothing" {
  rm "$REL/$PIN/checksums.txt"
  install_athome
  [ "$status" -eq 0 ]
  [[ "$output" == *"cannot download file://$REL/$PIN/checksums.txt"* ]]
  [ ! -e "$DEST" ]
}

@test "an unreachable server exits 0 with a note" {
  ATHOME_RELEASE_BASE=http://127.0.0.1:9 install_athome
  [ "$status" -eq 0 ]
  [[ "$output" == *"athome: cannot download http://127.0.0.1:9/$PIN/$ASSET"* ]]
  [ ! -e "$DEST" ]
}

# shim_path <dir> <tool...>: a PATH of only the named tools, plus fakes already in dir.
shim_path() {
  local dir="$1" tool
  shift
  mkdir -p "$dir"
  for tool in "$@"; do ln -sf "$(command -v "$tool")" "$dir/$tool"; done
}

BASE_TOOLS=(bash curl awk cut mktemp cp chmod mv mkdir rm tr cat)

# fake_uname <dir> <kernel> <machine>
fake_uname() {
  printf '#!/bin/sh\ncase "$1" in -s) echo %s ;; -m) echo %s ;; esac\n' "$2" "$3" >"$1/uname"
  chmod 0755 "$1/uname"
}

@test "aarch64 maps to the arm64 asset" {
  bin="$BATS_TEST_TMPDIR/bin"
  shim_path "$bin" "${BASE_TOOLS[@]}" shasum
  fake_uname "$bin" Linux aarch64
  fake_athome "$REL/$PIN/athome_linux_arm64" "${PIN#v}"
  (cd "$REL/$PIN" && shasum -a 256 athome_linux_arm64 >checksums.txt)
  HOME="$FAKEHOME" PATH="$bin" run bash "$SCRIPT"
  [ "$status" -eq 0 ]
  [ "$("$DEST" version)" = "athome ${PIN#v} (abc1234)" ]
}

@test "without sha256sum, shasum -a 256 checks the download" {
  bin="$BATS_TEST_TMPDIR/bin"
  shim_path "$bin" "${BASE_TOOLS[@]}" shasum uname
  [ ! -e "$bin/sha256sum" ]
  HOME="$FAKEHOME" PATH="$bin" run bash "$SCRIPT"
  [ "$status" -eq 0 ]
  [ -x "$DEST" ]
  echo "$(printf '0%.0s' $(seq 64))  $ASSET" >"$REL/$PIN/checksums.txt"
  rm "$DEST"
  HOME="$FAKEHOME" PATH="$bin" run bash "$SCRIPT"
  [ "$status" -eq 1 ]
  [ ! -e "$DEST" ]
}

@test "with neither sha256sum nor shasum, exits 1 and installs nothing" {
  bin="$BATS_TEST_TMPDIR/bin"
  shim_path "$bin" "${BASE_TOOLS[@]}" uname
  HOME="$FAKEHOME" PATH="$bin" run bash "$SCRIPT"
  [ "$status" -eq 1 ]
  [[ "$output" == *"neither sha256sum nor shasum found"* ]]
  [ ! -e "$DEST" ]
}

@test "an unsupported os or arch exits 0 with a note" {
  bin="$BATS_TEST_TMPDIR/bin"
  shim_path "$bin" "${BASE_TOOLS[@]}" shasum
  fake_uname "$bin" Linux riscv64
  HOME="$FAKEHOME" PATH="$bin" run bash "$SCRIPT"
  [ "$status" -eq 0 ]
  [[ "$output" == *"no release for riscv64"* ]]
  fake_uname "$bin" FreeBSD amd64
  HOME="$FAKEHOME" PATH="$bin" run bash "$SCRIPT"
  [ "$status" -eq 0 ]
  [[ "$output" == *"no release for FreeBSD"* ]]
  [ ! -e "$DEST" ]
}

@test "a pin that is not vX.Y.Z exits 1" {
  perl -pi -e 's/^pin=".*"/pin="main"/' "$SCRIPT"
  install_athome
  [ "$status" -eq 1 ]
  [[ "$output" == *"pins.athome main is not a vX.Y.Z release"* ]]
  [ ! -e "$DEST" ]
}

@test "personal, work and remote machines all run it; shellcheck and shfmt clean" {
  printf '[data]\n  machineType = "remote"\n  machineName = "box"\n  scope = "work/x"\n' >"$BATS_TEST_TMPDIR/remote.toml"
  for t in personal work remote; do
    [ -f "$BATS_TEST_TMPDIR/$t.toml" ] || printf '[data]\n  machineType = "%s"\n' "$t" >"$BATS_TEST_TMPDIR/$t.toml"
    chezmoi --source "$ROOT/home" --config "$BATS_TEST_TMPDIR/$t.toml" --destination "$BATS_TEST_TMPDIR/dest-$t" \
      --persistent-state "$BATS_TEST_TMPDIR/state-$t.boltdb" managed --include=scripts >"$BATS_TEST_TMPDIR/scripts-$t"
    grep -qx '.chezmoiscripts/14-install-athome.sh' "$BATS_TEST_TMPDIR/scripts-$t"
  done
  if command -v shellcheck >/dev/null; then shellcheck -S style "$SCRIPT"; fi
  if command -v shfmt >/dev/null; then shfmt -i 2 -ci -bn -d "$SCRIPT"; fi
}

@test "a pin change changes the rendered script" {
  src="$BATS_TEST_TMPDIR/src"
  cp -R "$ROOT/home" "$src"
  perl -pi -e 's/^athome = ".*"/athome = "v9.9.9"/' "$src/.chezmoidata/pins.toml"
  chezmoi --source "$src" --config "$BATS_TEST_TMPDIR/empty.toml" execute-template <"$TMPL" >"$BATS_TEST_TMPDIR/changed.sh"
  run ! cmp -s "$SCRIPT" "$BATS_TEST_TMPDIR/changed.sh"
  grep -qx 'pin="v9.9.9"' "$BATS_TEST_TMPDIR/changed.sh"
}
