#!/usr/bin/env bats
# The generated ~/.ssh/athome/config, read by ssh through the Include line.
# `ssh -G` prints the resolved options without connecting.

setup() {
  ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  FAKE_HOME="$BATS_TEST_TMPDIR/home"
  mkdir -p "$FAKE_HOME/.ssh/athome"
  cp "$ROOT/internal/machines/testdata/ssh-config.golden" "$FAKE_HOME/.ssh/athome/config"
  printf 'Include athome/config\n' >"$FAKE_HOME/.ssh/config"
}

# ssh resolves a relative Include and "~" through the passwd entry, not $HOME,
# so HOME is set for ssh alone and the user's real config is never read (-F).
ssh_g() { HOME="$FAKE_HOME" ssh -G -F "$FAKE_HOME/.ssh/config" "$1"; }

@test "acme-dev resolves through the Include line" {
  run ssh_g acme-dev
  [ "$status" -eq 0 ]
  [[ "$output" == *$'\nhostname dev.acme.example\n'* ]]
  [[ "$output" == *$'\nuser dev\n'* ]]
  [[ "$output" == *$'\nport 22\n'* ]]
  [[ "$output" == *$'\nhostkeyalias acme-dev\n'* ]]
  echo "$output" | grep -q '^identityfile .*/.ssh/laptop_ed25519$'
  echo "$output" | grep -q '^userknownhostsfile .*/.ssh/athome/known_hosts$'
  echo "$output" | grep -qx "sendenv CANOPY_THEME"
}

@test "a machine with a jump and forward_agent" {
  run ssh_g acme-gpu
  [ "$status" -eq 0 ]
  echo "$output" | grep -qx "port 2222"
  echo "$output" | grep -qx "proxyjump bastion"
  echo "$output" | grep -qx "forwardagent yes"
}
