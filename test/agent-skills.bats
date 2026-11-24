#!/usr/bin/env bats
# Step 4 of run_before_20-agent-skills: which vendored skills get switched on.
# The template is rendered with `chezmoi execute-template` and run against a
# temp HOME whose ~/.claude is a fake clone (a .git dir, so step 1 is skipped).

bats_require_minimum_version 1.5.0

setup() {
  unset TMUX GROFE ATHOME_CI
  ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  command -v chezmoi >/dev/null || skip "chezmoi not installed"
  command -v python3 >/dev/null || skip "python3 not installed"
  TMP="$BATS_TEST_TMPDIR"
  cat >"$TMP/cfg.toml" <<'TOML'
[data]
  machineType    = "personal"
  include_agents = true
  agents_repo    = "example/agents"
TOML
  chezmoi --source "$ROOT/home" --config "$TMP/cfg.toml" execute-template \
    <"$ROOT/home/.chezmoiscripts/run_before_20-agent-skills.sh.tmpl" >"$TMP/skills.sh"

  export HOME="$TMP/home"
  claude="$HOME/.claude"
  vendored="$claude/agent-skills/skills"
  active="$HOME/.config/agents/skills"
  mkdir -p "$claude/.git" "$active"
  for s in locked own stale; do
    mkdir -p "$vendored/$s"
    echo "# $s" >"$vendored/$s/SKILL.md"
  done
  mkdir -p "$vendored/nofile"
  printf '{"skills": {"locked": {}}}\n' >"$claude/agent-skills/.skill-lock.json"
  printf '# my own skills\nown\n\nnofile\n' >"$claude/agent-skills/.own-skills"

  mkdir -p "$active/mine"
  echo "# mine" >"$active/mine/SKILL.md"
  ln -s "$vendored/stale" "$active/stale"
  ln -s ../../../.claude/agent-skills/skills/own "$active/own"
}

@test "agent skills: links lock and own skills, skips one without SKILL.md, prunes stale" {
  run bash "$TMP/skills.sh"
  [ "$status" -eq 0 ]
  [ "$(readlink "$active/locked")" = "$vendored/locked" ]
  [ "$(readlink "$active/own")" = "$vendored/own" ]
  [ ! -e "$active/nofile" ] && [ ! -L "$active/nofile" ]
  [ ! -e "$active/stale" ] && [ ! -L "$active/stale" ]
  [ -d "$active/mine" ] && [ ! -L "$active/mine" ]
  [ "$(cat "$active/mine/SKILL.md")" = "# mine" ]
  [[ "$output" == *"1 from the lock, 1 of your own"* ]]
}

@test "agent skills: a second run changes nothing" {
  bash "$TMP/skills.sh" >/dev/null
  before="$(cd "$active" && ls -la | awk '{print $1, $NF}'; find . -type l -exec readlink {} \;)"
  run bash "$TMP/skills.sh"
  [ "$status" -eq 0 ]
  after="$(cd "$active" && ls -la | awk '{print $1, $NF}'; find . -type l -exec readlink {} \;)"
  [ "$before" = "$after" ]
}

@test "agent skills: without .own-skills only the lock is linked" {
  rm "$claude/agent-skills/.own-skills"
  run bash "$TMP/skills.sh"
  [ "$status" -eq 0 ]
  [ -L "$active/locked" ]
  [ "$(readlink "$active/own")" = ../../../.claude/agent-skills/skills/own ]
}
