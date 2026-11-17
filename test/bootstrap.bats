#!/usr/bin/env bats
# Tests for scripts/bootstrap.sh (the installer). Everything runs in a temp HOME.

setup() {
  unset TMUX GROFE ATHOME_CI GITHUB_TOKEN ATHOME_VERSION
  ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  export HOME="$BATS_TEST_TMPDIR/home"
  export TMPDIR="$BATS_TEST_TMPDIR/tmp"
  mkdir -p "$HOME" "$TMPDIR"
  case "$(uname -s)" in Darwin) OS=darwin ;; *) OS=linux ;; esac
  case "$(uname -m)" in x86_64 | amd64) ARCH=amd64 ;; *) ARCH=arm64 ;; esac
  ASSET="athome_${OS}_${ARCH}"
  REL="$BATS_TEST_TMPDIR/rel"
  mkdir -p "$REL/v9.9.9"
  make_fake "$REL/v9.9.9/$ASSET" v9.9.9
  (cd "$REL/v9.9.9" && shasum -a 256 "$ASSET" >checksums.txt)
  export ATHOME_RELEASE_BASE="file://$REL"
  export ATHOME_VERSION=v9.9.9
  DEST="$HOME/.local/bin/athome"
}

make_fake() {
  cat >"$1" <<FAKE
#!/bin/sh
if [ "\$1" = version ]; then echo "athome $2 (abc)"; exit 0; fi
echo "init-args:\$*" >> "\$HOME/calls"
FAKE
  chmod 0755 "$1"
}

@test "downloads, verifies, installs 0755 and execs init with args" {
  run bash "$ROOT/scripts/bootstrap.sh" --machine work -y
  [ "$status" -eq 0 ]
  [ -x "$DEST" ]
  [[ "$(ls -l "$DEST")" == -rwxr-xr-x* ]]
  grep -qx 'init-args:init --machine work -y' "$HOME/calls"
  [ -z "$(ls -d "$TMPDIR"/athome-install.* 2>/dev/null)" ]
}

@test "checksum mismatch exits 1 and leaves no binary" {
  echo "tampered" >>"$REL/v9.9.9/$ASSET"
  run bash "$ROOT/scripts/bootstrap.sh"
  [ "$status" -eq 1 ]
  [[ "$output" == *"checksum mismatch"* ]]
  [ ! -e "$DEST" ]
  [ -z "$(ls -A "$HOME/.local/bin")" ]
}

@test "missing asset falls back to bootstrap-legacy.sh with a note and the same args" {
  mkdir -p "$BATS_TEST_TMPDIR/lclone/scripts"
  cp "$ROOT/scripts/bootstrap.sh" "$BATS_TEST_TMPDIR/lclone/scripts/"
  printf '#!/bin/sh\necho "legacy-args:$*" >> "$HOME/calls"\n' >"$BATS_TEST_TMPDIR/lclone/scripts/bootstrap-legacy.sh"
  chmod 0755 "$BATS_TEST_TMPDIR/lclone/scripts/bootstrap-legacy.sh"
  ATHOME_VERSION=v0.0.1 run bash "$BATS_TEST_TMPDIR/lclone/scripts/bootstrap.sh" --machine work -y
  [ "$status" -eq 0 ]
  [[ "$output" == *"no athome release for v0.0.1; falling back to scripts/bootstrap-legacy.sh for this release"* ]]
  grep -qx 'legacy-args:--machine work -y' "$HOME/calls"
  [ ! -e "$DEST" ]
  [ -z "$(ls -d "$TMPDIR"/athome-install.* 2>/dev/null)" ]
}

@test "an unreachable release base dies naming the URL, without falling back" {
  mkdir -p "$BATS_TEST_TMPDIR/lclone/scripts"
  cp "$ROOT/scripts/bootstrap.sh" "$BATS_TEST_TMPDIR/lclone/scripts/"
  printf '#!/bin/sh\necho "legacy-args:$*" >> "$HOME/calls"\n' >"$BATS_TEST_TMPDIR/lclone/scripts/bootstrap-legacy.sh"
  chmod 0755 "$BATS_TEST_TMPDIR/lclone/scripts/bootstrap-legacy.sh"
  ATHOME_RELEASE_BASE=http://127.0.0.1:9 run bash "$BATS_TEST_TMPDIR/lclone/scripts/bootstrap.sh" -y
  [ "$status" -eq 1 ]
  [[ "$output" == *"cannot download http://127.0.0.1:9/v9.9.9/$ASSET"* ]]
  [[ "$output" != *"falling back"* ]]
  [ ! -e "$HOME/calls" ]
  [ ! -e "$DEST" ]
  [ -z "$(ls -d "$TMPDIR"/athome-install.* 2>/dev/null)" ]
}

# A fake curl first on PATH, faithful to how bootstrap.sh calls it: the asset
# download passes -w '%{http_code}' and reads the code from stdout, and curl -f
# exits 22 on an HTTP error. FAKE_ASSET_CODE is the status for the asset request;
# FAKE_CHECKSUMS_FAIL=1 makes the checksums.txt request fail.
fake_curl() {
  mkdir -p "$BATS_TEST_TMPDIR/curlbin"
  cat >"$BATS_TEST_TMPDIR/curlbin/curl" <<'CURL'
#!/bin/sh
code="" out=""
while [ $# -gt 0 ]; do
  case "$1" in
    -w) code=1; shift ;;
    -o) out="$2"; shift ;;
  esac
  shift
done
if [ -n "$code" ]; then
  printf '%s' "$FAKE_ASSET_CODE"
  if [ "$FAKE_ASSET_CODE" != 200 ]; then
    echo "curl: (22) The requested URL returned error: $FAKE_ASSET_CODE" >&2
    exit 22
  fi
  echo fake >"$out"
  exit 0
fi
echo "curl: (22) checksums gone" >&2
exit 22
CURL
  chmod 0755 "$BATS_TEST_TMPDIR/curlbin/curl"
  export PATH="$BATS_TEST_TMPDIR/curlbin:$PATH"
}

legacy_clone() {
  mkdir -p "$BATS_TEST_TMPDIR/lclone/scripts"
  cp "$ROOT/scripts/bootstrap.sh" "$BATS_TEST_TMPDIR/lclone/scripts/"
  printf '#!/bin/sh\necho "legacy-args:$*" >> "$HOME/calls"\n' >"$BATS_TEST_TMPDIR/lclone/scripts/bootstrap-legacy.sh"
  chmod 0755 "$BATS_TEST_TMPDIR/lclone/scripts/bootstrap-legacy.sh"
}

@test "an HTTP 404 for the asset falls back to bootstrap-legacy.sh" {
  fake_curl
  legacy_clone
  FAKE_ASSET_CODE=404 ATHOME_RELEASE_BASE=https://releases.invalid run bash "$BATS_TEST_TMPDIR/lclone/scripts/bootstrap.sh" -y
  [ "$status" -eq 0 ]
  [[ "$output" == *"falling back to scripts/bootstrap-legacy.sh"* ]]
  grep -qx 'legacy-args:-y' "$HOME/calls"
  [ ! -e "$DEST" ]
}

@test "an HTTP 500 for the asset exits 1 with curl's message and does not fall back" {
  fake_curl
  legacy_clone
  FAKE_ASSET_CODE=500 ATHOME_RELEASE_BASE=https://releases.invalid run bash "$BATS_TEST_TMPDIR/lclone/scripts/bootstrap.sh" -y
  [ "$status" -eq 1 ]
  [[ "$output" == *"cannot download https://releases.invalid/v9.9.9/$ASSET"* ]]
  [[ "$output" == *"returned error: 500"* ]]
  [[ "$output" != *"falling back"* ]]
  [ ! -e "$HOME/calls" ]
  [ ! -e "$DEST" ]
  [ -z "$(ls -d "$TMPDIR"/athome-install.* 2>/dev/null)" ]
}

@test "a failed checksums.txt download exits 1 with curl's message" {
  fake_curl
  FAKE_ASSET_CODE=200 ATHOME_RELEASE_BASE=https://releases.invalid run bash "$ROOT/scripts/bootstrap.sh"
  [ "$status" -eq 1 ]
  [[ "$output" == *"cannot download https://releases.invalid/v9.9.9/checksums.txt: curl: (22) checksums gone"* ]]
  [ ! -e "$DEST" ]
}

@test "the installer exports ATHOME_REPO and works from another directory" {
  make_fake "$BATS_TEST_TMPDIR/athome-src" vX
  cat >"$BATS_TEST_TMPDIR/athome-src" <<'STUB'
#!/bin/sh
echo "repo:$ATHOME_REPO" >> "$HOME/calls"
STUB
  mkdir "$BATS_TEST_TMPDIR/elsewhere"
  cd "$BATS_TEST_TMPDIR/elsewhere"
  run bash "$ROOT/scripts/bootstrap.sh" --install-from "$BATS_TEST_TMPDIR/athome-src"
  [ "$status" -eq 0 ]
  grep -qx "repo:$ROOT" "$HOME/calls"
}

@test "no curl exits 1 naming curl" {
  mkdir "$BATS_TEST_TMPDIR/nobin"
  for t in bash env cut awk mktemp mkdir rm cp mv chmod git dirname uname cat shasum; do
    p="$(command -v $t)" && ln -s "$p" "$BATS_TEST_TMPDIR/nobin/$t"
  done
  PATH="$BATS_TEST_TMPDIR/nobin" run "$BATS_TEST_TMPDIR/nobin/bash" "$ROOT/scripts/bootstrap.sh"
  [ "$status" -eq 1 ]
  [[ "$output" == *"curl"* ]]
  [ ! -e "$DEST" ]
}

@test "checksums.txt without the asset line exits 1" {
  echo "deadbeef  other" >"$REL/v9.9.9/checksums.txt"
  run bash "$ROOT/scripts/bootstrap.sh"
  [ "$status" -eq 1 ]
  [[ "$output" == *"no line for $ASSET"* ]]
  [ ! -e "$DEST" ]
}

@test "unsupported arch exits 1" {
  mkdir "$BATS_TEST_TMPDIR/fakebin"
  printf '#!/bin/sh\n[ "$1" = -m ] && echo riscv64 || /usr/bin/uname "$@"\n' >"$BATS_TEST_TMPDIR/fakebin/uname"
  chmod +x "$BATS_TEST_TMPDIR/fakebin/uname"
  PATH="$BATS_TEST_TMPDIR/fakebin:$PATH" run bash "$ROOT/scripts/bootstrap.sh"
  [ "$status" -eq 1 ]
  [[ "$output" == *"unsupported arch"* ]]
  [ ! -e "$DEST" ]
}

@test "unsupported os exits 1" {
  mkdir "$BATS_TEST_TMPDIR/fakebin"
  printf '#!/bin/sh\n[ "$1" = -s ] && echo FreeBSD || /usr/bin/uname "$@"\n' >"$BATS_TEST_TMPDIR/fakebin/uname"
  chmod +x "$BATS_TEST_TMPDIR/fakebin/uname"
  PATH="$BATS_TEST_TMPDIR/fakebin:$PATH" run bash "$ROOT/scripts/bootstrap.sh"
  [ "$status" -eq 1 ]
  [[ "$output" == *"unsupported os"* ]]
  [ ! -e "$DEST" ]
}

@test "no sha tool exits 1 naming both" {
  mkdir "$BATS_TEST_TMPDIR/nobin"
  for t in bash env cut awk mktemp curl mkdir rm cp mv chmod git dirname uname cat; do
    p="$(command -v $t)" && ln -s "$p" "$BATS_TEST_TMPDIR/nobin/$t"
  done
  PATH="$BATS_TEST_TMPDIR/nobin" run "$BATS_TEST_TMPDIR/nobin/bash" "$ROOT/scripts/bootstrap.sh"
  [ "$status" -eq 1 ]
  [[ "$output" == *"sha256sum"* && "$output" == *"shasum"* ]]
  [ ! -e "$DEST" ]
}

@test "--install-from installs that file (path with spaces) without downloading" {
  mkdir "$BATS_TEST_TMPDIR/with space"
  make_fake "$BATS_TEST_TMPDIR/with space/athome" vX
  ATHOME_RELEASE_BASE=file:///nonexistent run bash "$ROOT/scripts/bootstrap.sh" --install-from "$BATS_TEST_TMPDIR/with space/athome" --foo
  [ "$status" -eq 0 ]
  grep -qx 'init-args:init --foo' "$HOME/calls"
  [ "$("$DEST" version)" = "athome vX (abc)" ]
}

@test "--install-from with missing path or no path exits 1" {
  run bash "$ROOT/scripts/bootstrap.sh" --install-from "$BATS_TEST_TMPDIR/nope"
  [ "$status" -eq 1 ]
  [ ! -e "$DEST" ]
  run bash "$ROOT/scripts/bootstrap.sh" --install-from
  [ "$status" -eq 1 ]
  run bash "$ROOT/scripts/bootstrap.sh" --install-from ""
  [ "$status" -eq 1 ]
  [[ "$output" == *"--install-from needs a path"* ]]
  [ ! -e "$DEST" ]
}

@test "installed binary at the wanted version is reused offline" {
  mkdir -p "$HOME/.local/bin"
  make_fake "$DEST" v9.9.9
  ATHOME_RELEASE_BASE=file:///nonexistent run bash "$ROOT/scripts/bootstrap.sh" -y
  [ "$status" -eq 0 ]
  grep -qx 'init-args:init -y' "$HOME/calls"
}

@test "installed binary at another version is replaced" {
  mkdir -p "$HOME/.local/bin"
  make_fake "$DEST" v1.0.0
  run bash "$ROOT/scripts/bootstrap.sh"
  [ "$status" -eq 0 ]
  [ "$("$DEST" version)" = "athome v9.9.9 (abc)" ]
}

@test "broken installed binary is replaced" {
  mkdir -p "$HOME/.local/bin"
  printf '#!/bin/sh\nexit 3\n' >"$DEST"
  chmod +x "$DEST"
  run bash "$ROOT/scripts/bootstrap.sh"
  [ "$status" -eq 0 ]
  [ "$("$DEST" version)" = "athome v9.9.9 (abc)" ]
}

@test "--help prints usage and exits 0" {
  run bash "$ROOT/scripts/bootstrap.sh" --help
  [ "$status" -eq 0 ]
  [[ "$output" == *"--install-from"* ]]
  [ ! -e "$DEST" ]
}

@test "no tag and no ATHOME_VERSION exits 1 naming --install-from" {
  unset ATHOME_VERSION
  mkdir -p "$BATS_TEST_TMPDIR/clone/scripts"
  cp "$ROOT/scripts/bootstrap.sh" "$BATS_TEST_TMPDIR/clone/scripts/"
  git -C "$BATS_TEST_TMPDIR/clone" init -q
  run bash "$BATS_TEST_TMPDIR/clone/scripts/bootstrap.sh"
  [ "$status" -eq 1 ]
  [[ "$output" == *"--install-from"* ]]
  [ ! -e "$DEST" ]
}

# A clone with tags v1.0.0 and v1.1.0; only the release dir for $1 exists, so a
# successful install proves that tag was requested.
tagged_clone() {
  local c="$BATS_TEST_TMPDIR/tclone" g="git -C $BATS_TEST_TMPDIR/tclone -c user.name=t -c user.email=t@t -c commit.gpgsign=false -c tag.gpgsign=false"
  mkdir -p "$c/scripts"
  cp "$ROOT/scripts/bootstrap.sh" "$c/scripts/"
  git -C "$c" init -q
  $g add . && $g commit -q -m one && $g tag v1.0.0
  $g commit -q --allow-empty -m two && $g tag v1.1.0
  rm -rf "$REL"
  mkdir -p "$REL/$1"
  make_fake "$REL/$1/$ASSET" "$1"
  (cd "$REL/$1" && shasum -a 256 "$ASSET" >checksums.txt)
}

@test "tag exactly at HEAD is the version installed" {
  unset ATHOME_VERSION
  tagged_clone v1.1.0
  run bash "$BATS_TEST_TMPDIR/tclone/scripts/bootstrap.sh"
  [ "$status" -eq 0 ]
  [ "$("$DEST" version)" = "athome v1.1.0 (abc)" ]
}

@test "HEAD past a tag installs the nearest tag" {
  unset ATHOME_VERSION
  tagged_clone v1.1.0
  git -C "$BATS_TEST_TMPDIR/tclone" -c user.name=t -c user.email=t@t -c commit.gpgsign=false commit -q --allow-empty -m three
  run bash "$BATS_TEST_TMPDIR/tclone/scripts/bootstrap.sh"
  [ "$status" -eq 0 ]
  [ "$("$DEST" version)" = "athome v1.1.0 (abc)" ]
}

@test "ATHOME_VERSION overrides a tag at HEAD" {
  tagged_clone v1.0.0
  ATHOME_VERSION=v1.0.0 run bash "$BATS_TEST_TMPDIR/tclone/scripts/bootstrap.sh"
  [ "$status" -eq 0 ]
  [ "$("$DEST" version)" = "athome v1.0.0 (abc)" ]
}

@test "args before and after --install-from reach init in order" {
  make_fake "$BATS_TEST_TMPDIR/athome-src" vX
  run bash "$ROOT/scripts/bootstrap.sh" -y --install-from "$BATS_TEST_TMPDIR/athome-src" --machine work
  [ "$status" -eq 0 ]
  grep -qx 'init-args:init -y --machine work' "$HOME/calls"
}
