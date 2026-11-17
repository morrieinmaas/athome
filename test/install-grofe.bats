#!/usr/bin/env bats
# Tests for home/.chezmoiscripts/run_onchange_17-install-grofe.sh.tmpl. The
# template is rendered against home/.chezmoidata/pins.toml, then run with a
# fake HOME and a file:// release base. No network.

setup() {
  unset TMUX GROFE ATHOME_GROFE_BIN
  ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  command -v chezmoi >/dev/null || skip "chezmoi not installed"
  export TMPDIR="$BATS_TEST_TMPDIR/tmp"
  FAKEHOME="$BATS_TEST_TMPDIR/fh"
  mkdir -p "$TMPDIR" "$FAKEHOME"
  : >"$BATS_TEST_TMPDIR/empty.toml"
  SCRIPT="$BATS_TEST_TMPDIR/install.sh"
  chezmoi --source "$ROOT/home" --config "$BATS_TEST_TMPDIR/empty.toml" execute-template \
    <"$ROOT/home/.chezmoiscripts/run_onchange_17-install-grofe.sh.tmpl" >"$SCRIPT"
  PIN="$(awk -F'"' '/^grofe/ { print $2 }' "$ROOT/home/.chezmoidata/pins.toml")"
  case "$(uname -s)-$(uname -m)" in
    Linux-x86_64) TARGET=x86_64-unknown-linux-musl ;;
    Linux-aarch64 | Linux-arm64) TARGET=aarch64-unknown-linux-musl ;;
    Darwin-x86_64) TARGET=x86_64-apple-darwin ;;
    *) TARGET=aarch64-apple-darwin ;;
  esac
  NAME="grofe-$PIN-$TARGET"
  REL="$BATS_TEST_TMPDIR/rel"
  mkdir -p "$REL/$PIN" "$BATS_TEST_TMPDIR/pkg/$NAME"
  printf '#!/bin/sh\necho "grofe 0.3.1"\n' >"$BATS_TEST_TMPDIR/pkg/$NAME/grofe"
  chmod 0755 "$BATS_TEST_TMPDIR/pkg/$NAME/grofe"
  echo license >"$BATS_TEST_TMPDIR/pkg/$NAME/LICENSE"
  tar -czf "$REL/$PIN/$NAME.tar.gz" -C "$BATS_TEST_TMPDIR/pkg" "$NAME"
  (cd "$REL/$PIN" && shasum -a 256 "$NAME.tar.gz" >"$NAME.tar.gz.sha256")
  export GROFE_RELEASE_BASE="file://$REL"
}

install_grofe() {
  HOME="$FAKEHOME" run bash "$SCRIPT"
}

@test "a good tarball installs grofe and the tmux link" {
  install_grofe
  [ "$status" -eq 0 ]
  [ -x "$FAKEHOME/.local/lib/grofe/grofe" ]
  [ -L "$FAKEHOME/.local/lib/grofe/bin/tmux" ]
  [ -L "$FAKEHOME/.local/lib/grofe/bin/grofe" ]
  [ -z "$(ls -d "$TMPDIR"/grofe-install.* 2>/dev/null)" ]
}

@test "a wrong sha256 file exits 1 and installs nothing" {
  echo "$(printf '0%.0s' $(seq 64))  $NAME.tar.gz" >"$REL/$PIN/$NAME.tar.gz.sha256"
  install_grofe
  [ "$status" -eq 1 ]
  [[ "$output" == *"checksum mismatch"* ]]
  [ ! -e "$FAKEHOME/.local/lib/grofe/grofe" ]
}

@test "a missing sha256 file exits 1 and installs nothing" {
  rm "$REL/$PIN/$NAME.tar.gz.sha256"
  install_grofe
  [ "$status" -eq 1 ]
  [ ! -e "$FAKEHOME/.local/lib/grofe/grofe" ]
}

@test "a missing tarball exits 0 with the tmux-stays-tmux note" {
  rm "$REL/$PIN/$NAME.tar.gz"
  install_grofe
  [ "$status" -eq 0 ]
  [[ "$output" == *"grofe: cannot download file://$REL/$PIN/$NAME.tar.gz, so grofe is not installed; tmux stays tmux"* ]]
  [ ! -e "$FAKEHOME/.local/lib/grofe/grofe" ]
}

@test "ATHOME_GROFE_BIN wins over the download" {
  printf '#!/bin/sh\necho "grofe local"\n' >"$BATS_TEST_TMPDIR/mine"
  chmod 0755 "$BATS_TEST_TMPDIR/mine"
  rm -rf "$REL"
  ATHOME_GROFE_BIN="$BATS_TEST_TMPDIR/mine" install_grofe
  [ "$status" -eq 0 ]
  [[ "$("$FAKEHOME/.local/lib/grofe/grofe")" == "grofe local" ]]
}

@test "a pin change changes the rendered script, and both scripts carry only the pins" {
  sed_pin="$BATS_TEST_TMPDIR/src"
  mkdir -p "$sed_pin"
  cp -R "$ROOT/home/.chezmoidata" "$ROOT/home/.chezmoiscripts" "$sed_pin/"
  perl -pi -e 's/^grofe = ".*"/grofe = "v9.9.9"/' "$sed_pin/.chezmoidata/pins.toml"
  chezmoi --source "$sed_pin" --config "$BATS_TEST_TMPDIR/empty.toml" execute-template \
    <"$ROOT/home/.chezmoiscripts/run_onchange_17-install-grofe.sh.tmpl" >"$BATS_TEST_TMPDIR/changed.sh"
  ! cmp -s "$SCRIPT" "$BATS_TEST_TMPDIR/changed.sh"
  grep -q 'v9.9.9' "$BATS_TEST_TMPDIR/changed.sh"
  CAN="$(chezmoi --source "$ROOT/home" --config "$BATS_TEST_TMPDIR/empty.toml" execute-template \
    <"$ROOT/home/.chezmoiscripts/run_onchange_18-install-canopy.sh.tmpl")"
  # no ref literal other than the rendered pins
  run bash -c 'grep -oE "[0-9a-f]{40}|v[0-9]+\.[0-9]+\.[0-9]+" "$1" "$2" | sort -u' _ "$SCRIPT" <(printf '%s' "$CAN")
  [[ "$output" == *"$PIN"* ]]
  [[ "$output" == *"$(awk -F'"' '/^canopy/ { print $2 }' "$ROOT/home/.chezmoidata/pins.toml")"* ]]
  [ "$(printf '%s\n' "$output" | wc -l)" -eq 2 ]
}
