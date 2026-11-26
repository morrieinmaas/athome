#!/usr/bin/env bats
# Files the user owns: ~/.zshrc, ~/.ssh/config, ~/.gitconfig and the themed
# configs. chezmoi keeps one athome line in the first two (modify_ scripts),
# only appends missing identity includes to ~/.gitconfig (modify_), and writes
# the themed configs only when missing (create_), so an apply never drops a
# line the user or a tool added. Every target is rendered with `chezmoi cat` against a
# temp destination, never applied.

bats_require_minimum_version 1.5.0

setup() {
  unset TMUX GROFE ATHOME_CI
  ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  command -v chezmoi >/dev/null || skip "chezmoi not installed"
  TMP="$BATS_TEST_TMPDIR"
  DEST="$TMP/home"
  mkdir -p "$DEST/.ssh"
  for t in personal work; do
    printf '[data]\n  machineType = "%s"\n' "$t" >"$TMP/$t.toml"
  done
  printf '[data]\n  machineType = "remote"\n  machineName = "box"\n  scope = "work/x"\n' >"$TMP/remote.toml"
  ZSH_HOOK='[[ -f "$HOME/.zsh/athome.zsh" ]] && source "$HOME/.zsh/athome.zsh"'
  SSH_HOOK='Include athome/*.conf'
}

# through <target under home> [config]: what apply would write there, given the
# file currently in $DEST.
through() {
  chezmoi --source "$ROOT/home" --config "$TMP/${2:-personal}.toml" \
    --destination "$DEST" --persistent-state "$TMP/state.db" cat "$DEST/$1"
}

# old_zshrc / old_ssh: what athome used to render as the whole file.
old_zshrc() { cat "$ROOT/test/fixtures/zshrc"; }
# The old ssh file is base.conf's body under the old header (templates.bats
# checks that against test/fixtures/ssh-config), rendered for this host.
old_ssh() {
  printf '# ~/.ssh/config — managed by chezmoi\n\n'
  chezmoi --source "$ROOT/home" --config "$TMP/${1:-personal}.toml" execute-template \
    <"$ROOT/home/private_dot_ssh/private_athome/private_base.conf.tmpl" | tail -n +8 |
    perl -pe 's{see ~/.config/git/config includeIf}{see ~/.gitconfig includeIf}'
}

# ── ~/.zshrc ─────────────────────────────────────────────────────────────────

@test "zshrc: a missing or empty file gets the hook line alone" {
  for t in personal work; do
    rm -f "$DEST/.zshrc"
    [ "$(through .zshrc "$t")" = "$ZSH_HOOK" ]
    : >"$DEST/.zshrc"
    [ "$(through .zshrc "$t")" = "$ZSH_HOOK" ]
  done
}

@test "zshrc: the old athome body becomes the hook, and lines a tool appended stay verbatim" {
  { old_zshrc; printf '\n# Scaleway CLI autocomplete initialization.\neval "$(scw autocomplete script shell=zsh)"\n'; } >"$DEST/.zshrc"
  run through .zshrc
  [ "$status" -eq 0 ]
  [ "$output" = "$ZSH_HOOK

# Scaleway CLI autocomplete initialization.
eval \"\$(scw autocomplete script shell=zsh)\"" ]
}

@test "zshrc: lines above the old body stay above the hook" {
  { printf 'export EARLY=1\n'; old_zshrc; printf 'alias late=1\n'; } >"$DEST/.zshrc"
  [ "$(through .zshrc)" = "export EARLY=1
$ZSH_HOOK
alias late=1" ]
}

@test "zshrc: the pre-bootstrap mise line install-mise.sh adds goes, the user's own stays" {
  grep -q 'activate zsh)"   # athome: pre-bootstrap stopgap' "$ROOT/scripts/install-mise.sh"
  printf '%s\n' 'eval "$(/usr/bin/mise activate zsh)"   # athome: pre-bootstrap stopgap' 'eval "$(mise activate zsh)"' >"$DEST/.zshrc"
  [ "$(through .zshrc)" = "$ZSH_HOOK
eval \"\$(mise activate zsh)\"" ]
}

@test "zshrc: a file of the user's own gets the hook on top, everything else verbatim" {
  printf 'export A=1\n\n  # indented comment\neval "$(tool init zsh)"\n' >"$DEST/.zshrc"
  [ "$(through .zshrc)" = "$ZSH_HOOK
export A=1

  # indented comment
eval \"\$(tool init zsh)\"" ]
}

@test "zshrc: a hook already there, anywhere, is left alone and never doubled" {
  printf 'export A=1\nsource ~/.zsh/athome.zsh\neval "$(x)"\nsource ~/.zsh/athome.zsh\n' >"$DEST/.zshrc"
  [ "$(through .zshrc)" = 'export A=1
source ~/.zsh/athome.zsh
eval "$(x)"' ]
}

@test "zshrc: a commented-out hook does not count" {
  printf '# source ~/.zsh/athome.zsh\n' >"$DEST/.zshrc"
  [ "$(through .zshrc)" = "$ZSH_HOOK
# source ~/.zsh/athome.zsh" ]
}

@test "zshrc: idempotent, a second apply changes nothing" {
  for input in "" "$(old_zshrc)" "$(printf 'x=1\n%s\ny=2' "$(old_zshrc)")" 'eval "$(scw autocomplete script shell=zsh)"'; do
    printf '%s\n' "$input" >"$DEST/.zshrc"
    through .zshrc >"$TMP/once"
    cp "$TMP/once" "$DEST/.zshrc"
    through .zshrc >"$TMP/twice"
    cmp "$TMP/once" "$TMP/twice"
    [ "$(grep -c 'zsh/athome.zsh' "$TMP/once")" -eq 1 ]
  done
}

@test "zshrc: the hook output parses, and athome.zsh is the old body plus the drift notice" {
  command -v zsh >/dev/null || skip "zsh not installed"
  rm -f "$DEST/.zshrc"
  through .zshrc >"$TMP/zshrc"
  zsh -n "$TMP/zshrc"
  # Same body as the old ~/.zshrc, apart from the header comment and drift-notice.zsh.
  diff <(tail -n +6 "$ROOT/home/dot_zsh/athome.zsh" | /usr/bin/sed 's/ drift-notice\.zsh; do$/; do/') \
    <(tail -n +4 "$ROOT/test/fixtures/zshrc")
}

# ── ~/.ssh/config ────────────────────────────────────────────────────────────

@test "ssh config: a missing or empty file gets the Include line alone" {
  for t in personal work; do
    rm -f "$DEST/.ssh/config"
    [ "$(through .ssh/config "$t")" = "$SSH_HOOK" ]
    : >"$DEST/.ssh/config"
    [ "$(through .ssh/config "$t")" = "$SSH_HOOK" ]
  done
}

@test "ssh config: the old athome body goes, the user's Host * / Include config.d stays verbatim" {
  { old_ssh; printf '\nHost *\n    Include config.d/*\n'; } >"$DEST/.ssh/config"
  [ "$(through .ssh/config)" = "$SSH_HOOK

Host *
    Include config.d/*" ]
}

@test "ssh config: the old remote body and the bootstrap's seeded blocks go too" {
  {
    printf 'Host mine\n    HostName mine.example\n'
    printf '\n# === seeded by scripts/lib/ssh.sh — the chezmoi-managed ~/.ssh/config replaces this ===\nHost github.com\n    HostName github.com\n    User git\n    IdentityFile ~/.ssh/id_ed25519\n    IdentityFile ~/.ssh/h_ed25519\n'
    printf '\n# === chezmoi run_before_00 seed — overwritten by canonical config later in this same apply ===\nHost github.com\n    HostName github.com\n    IdentityFile ~/.ssh/h_ed25519\n'
  } >"$DEST/.ssh/config"
  [ "$(through .ssh/config)" = "$SSH_HOOK
Host mine
    HostName mine.example" ]
  old_ssh remote >"$DEST/.ssh/config"
  grep -q 'StrictHostKeyChecking yes' "$DEST/.ssh/config"
  [ "$(through .ssh/config remote)" = "$SSH_HOOK" ]
}

@test "ssh config: the Include line goes above the first Host or Match, once" {
  printf '# mine\nHost a\n    User me\nInclude athome/*.conf\nMatch all\n' >"$DEST/.ssh/config"
  [ "$(through .ssh/config)" = "$SSH_HOOK
# mine
Host a
    User me
Match all" ]
}

@test "ssh config: an Include line already above every Host block is left where it is" {
  printf 'Include config.d/*\ninclude athome/*.conf\nHost a\n    User me\nInclude athome/*.conf\n' >"$DEST/.ssh/config"
  [ "$(through .ssh/config)" = 'Include config.d/*
include athome/*.conf
Host a
    User me' ]
}

@test "ssh config: idempotent, a second apply changes nothing" {
  for input in "" "$(old_ssh)" "$(printf '%s\n\nHost *\n    Include config.d/*' "$(old_ssh)")" 'Host a'; do
    printf '%s\n' "$input" >"$DEST/.ssh/config"
    through .ssh/config >"$TMP/once"
    cp "$TMP/once" "$DEST/.ssh/config"
    through .ssh/config >"$TMP/twice"
    cmp "$TMP/once" "$TMP/twice"
    [ "$(grep -ci '^include athome/\*\.conf$' "$TMP/once")" -eq 1 ]
  done
}

@test "ssh config: ssh resolves athome's github.com and the user's config.d hosts through the chain" {
  command -v ssh >/dev/null || skip "ssh not installed"
  mkdir -p "$DEST/.ssh/athome" "$DEST/.ssh/config.d"
  perl -pe 's/\.chezmoi\.hostname/"fixture"/g' "$ROOT/home/private_dot_ssh/private_athome/private_base.conf.tmpl" |
    chezmoi --source "$ROOT/home" --config "$TMP/personal.toml" execute-template >"$DEST/.ssh/athome/base.conf"
  printf 'Host acme-dev\n    HostName dev.acme.example\n' >"$DEST/.ssh/athome/config"
  printf 'Host mybox\n    HostName mybox.example\n' >"$DEST/.ssh/config.d/mine"
  { old_ssh; printf '\nHost *\n    Include config.d/*\n'; } >"$DEST/.ssh/config"
  through .ssh/config >"$TMP/config"
  cp "$TMP/config" "$DEST/.ssh/config"
  # Relative Includes resolve against the config file's own ~/.ssh: HOME for ssh alone.
  ssh_g() { HOME="$DEST" ssh -G -F "$DEST/.ssh/config" "$1"; }
  run ssh_g github.com
  [[ "$output" == *$'\nidentitiesonly yes\n'* ]]
  echo "$output" | grep -q '^identityfile .*/.ssh/fixture_ed25519$'
  run ssh_g acme-dev
  echo "$output" | grep -qx 'hostname dev.acme.example'
  run ssh_g mybox
  echo "$output" | grep -qx 'hostname mybox.example'
}

# ── git ──────────────────────────────────────────────────────────────────────

@test "git: athome's settings render into ~/.config/git/config on both OSes" {
  for t in personal work; do
    for os in darwin linux; do
      printf '[data]\n  machineType = "%s"\n  name_personal = "P"\n  email_personal = "p@example.org"\n' "$t" >"$TMP/g.toml"
      perl -pe "s/\.chezmoi\.os/\"$os\"/g" "$ROOT/home/dot_config/git/config.tmpl" |
        chezmoi --source "$ROOT/home" --config "$TMP/g.toml" execute-template >"$TMP/gitconfig"
      [ "$(git config -f "$TMP/gitconfig" user.email)" = "p@example.org" ]
      # shellcheck disable=SC2088 # the literal value git stores
      [ "$(git config -f "$TMP/gitconfig" includeIf.gitdir:~/work/.path)" = "~/.gitconfig-work" ]
      [ "$(git config -f "$TMP/gitconfig" url.git@github.com:.pushInsteadOf)" = "https://github.com/" ]
    done
  done
}

@test "git: ~/.gitconfig lines pass through verbatim; only missing identity includes are appended" {
  [ ! -e "$ROOT/home/dot_gitconfig.tmpl" ]
  # The old athome body already has [user] above its includeIf blocks: unchanged.
  printf '[data]\n  machineType = "personal"\n  name_personal = "P"\n  email_personal = "p@example.org"\n' >"$TMP/g.toml"
  { chezmoi --source "$ROOT/home" --config "$TMP/g.toml" execute-template <"$ROOT/test/fixtures/gitconfig.tmpl"
    printf '\tinsteadOf = https://github.com/\n'; } >"$DEST/.gitconfig"
  through .gitconfig >"$TMP/out"
  diff "$TMP/out" "$DEST/.gitconfig"
  # A file with no [user] (gh auth setup-git wrote it): [user] + all three appended.
  printf '[credential "https://github.com"]\n\thelper = !gh auth git-credential\n' >"$DEST/.gitconfig"
  through .gitconfig >"$TMP/out"
  [ "$(head -2 "$TMP/out")" = "$(cat "$DEST/.gitconfig")" ]
  [ "$(git config -f "$TMP/out" --get-regexp '^includeif\.' | wc -l)" -eq 3 ]
  grep -qx '\[user\]' "$TMP/out"
  # Idempotent.
  cp "$TMP/out" "$DEST/.gitconfig"
  through .gitconfig | cmp - "$DEST/.gitconfig"
  # Missing file: a header, an empty [user], the three includes.
  rm -f "$DEST/.gitconfig"
  through .gitconfig >"$TMP/out"
  [ "$(git config -f "$TMP/out" --get-regexp '^includeif\.' | wc -l)" -eq 3 ]
}

@test "git: a user.email set before the first apply does not beat the per-directory identity" {
  mkdir -p "$DEST/.config/git" "$DEST/work/r"
  printf '[data]\n  machineType = "personal"\n  name_personal = "P"\n  email_personal = "p@example.org"\n' >"$TMP/g.toml"
  chezmoi --source "$ROOT/home" --config "$TMP/g.toml" execute-template <"$ROOT/home/dot_config/git/config.tmpl" >"$DEST/.config/git/config"
  printf '[user]\n\temail = w@example.org\n' >"$DEST/.gitconfig-work"
  printf '[user]\n\temail = pre@example.org\n' >"$DEST/.gitconfig"
  through .gitconfig >"$TMP/out"
  cp "$TMP/out" "$DEST/.gitconfig"
  g() { HOME="$DEST" XDG_CONFIG_HOME="$DEST/.config" GIT_CONFIG_NOSYSTEM=1 git "$@"; }
  g -C "$DEST/work/r" init -q
  [ "$(g -C "$DEST/work/r" config user.email)" = "w@example.org" ]
}

@test "git: a value in ~/.gitconfig wins over athome's, and git config --global writes there" {
  mkdir -p "$DEST/.config/git"
  printf '[pull]\n\trebase = true\n' >"$DEST/.config/git/config"
  rm -f "$DEST/.gitconfig"; through .gitconfig >"$TMP/gc" && cp "$TMP/gc" "$DEST/.gitconfig"
  HOME="$DEST" XDG_CONFIG_HOME="$DEST/.config" git config --global pull.rebase false
  grep -q 'rebase = false' "$DEST/.gitconfig"
  run ! grep -q 'rebase = false' "$DEST/.config/git/config"
  [ "$(HOME="$DEST" XDG_CONFIG_HOME="$DEST/.config" git config pull.rebase)" = "false" ]
}

# ── themed configs ───────────────────────────────────────────────────────────

@test "themed configs: create_ only, so apply never resets a theme pick" {
  for f in gh-dash/config.yml slk/config.toml niri/config.kdl; do
    d="$ROOT/home/dot_config/${f%/*}"
    [ -f "$d/create_${f#*/}.tmpl" ]
    [ -z "$(ls "$d"/modify_* 2>/dev/null)" ]
  done
  mkdir -p "$DEST/.config/slk" "$DEST/.config/gh-dash"
  printf 'theme = "Everforest Light"\n' >"$DEST/.config/slk/config.toml"
  printf 'theme: mine\n' >"$DEST/.config/gh-dash/config.yml"
  chezmoi --source "$ROOT/home" --config "$TMP/personal.toml" --destination "$DEST" \
    --persistent-state "$TMP/state.db" status "$DEST/.config/slk/config.toml" "$DEST/.config/gh-dash/config.yml" >"$TMP/status"
  [ ! -s "$TMP/status" ]
}

@test "themed configs: a missing file gets the repo's version" {
  for t in personal work; do
    for f in gh-dash/config.yml niri/config.kdl; do
      chezmoi --source "$ROOT/home" --config "$TMP/$t.toml" execute-template <"$ROOT/home/dot_config/${f%/*}/create_${f#*/}.tmpl" >"$TMP/out"
      cmp "$TMP/out" "$ROOT/home/.chezmoitemplates/$f"
    done
    { cat "$TMP/$t.toml"; printf '  slack_default_workspace = ""\n  slack_team_id = ""\n'; } >"$TMP/slk.toml"
    chezmoi --source "$ROOT/home" --config "$TMP/slk.toml" execute-template <"$ROOT/home/dot_config/slk/create_config.toml.tmpl" | grep -q '^theme *= '
  done
}

# ── containers ───────────────────────────────────────────────────────────────

@test "containers: athome's registry settings are a drop-in, registries.conf stays the user's" {
  [ -f "$ROOT/home/dot_config/containers/registries.conf.d/athome.conf" ]
  [ ! -e "$ROOT/home/dot_config/containers/registries.conf" ]
  grep -qx 'unqualified-search-registries = \["docker.io"\]' "$ROOT/home/dot_config/containers/registries.conf.d/athome.conf"
}

# ── review follow-ups: lines athome never wrote survive migration ───────────

@test "zshrc: a line that only names athome.zsh is not the hook" {
  printf '%s\n' "alias ea='nvim ~/.zsh/athome.zsh'" 'source ~/.zsh/athome.zsh' >"$DEST/.zshrc"
  [ "$(through .zshrc)" = "alias ea='nvim ~/.zsh/athome.zsh'
source ~/.zsh/athome.zsh" ]
  printf '%s\n' "alias ea='nvim ~/.zsh/athome.zsh'" >"$DEST/.zshrc"
  [ "$(through .zshrc)" = "$ZSH_HOOK
alias ea='nvim ~/.zsh/athome.zsh'" ]
}

@test "zshrc: a line the user put inside the old body stays" {
  old_zshrc | awk 'NR == 50 { print "export MINE=1" } { print }' >"$DEST/.zshrc"
  [ "$(through .zshrc)" = "$ZSH_HOOK
export MINE=1" ]
}

@test "zshrc: an older athome body (different source loop) and a truncated one migrate too" {
  old_zshrc | perl -pe 's/ ssh-nudge.zsh canopy-theme.zsh//' >"$DEST/.zshrc"
  printf 'eval "$(scw autocomplete script shell=zsh)"\n' >>"$DEST/.zshrc"
  [ "$(through .zshrc)" = "$ZSH_HOOK
eval \"\$(scw autocomplete script shell=zsh)\"" ]
  { old_zshrc | head -60; printf 'alias mine=1\n'; } >"$DEST/.zshrc"
  [ "$(through .zshrc)" = "$ZSH_HOOK
alias mine=1" ]
}

@test "ssh config: a Host the user put inside the old body stays, with its options" {
  old_ssh | awk '/^Include config.local$/ { print; print "Host mine"; print "    HostName 192.0.2.1"; next } { print }' >"$DEST/.ssh/config"
  [ "$(through .ssh/config)" = "$SSH_HOOK
Host mine
    HostName 192.0.2.1" ]
}

@test "ssh config: an option the user added under the old github.com block keeps its Host line" {
  { old_ssh; printf '    Port 443\n'; } >"$DEST/.ssh/config"
  [ "$(through .ssh/config)" = "$SSH_HOOK
Host github.com
    Port 443" ]
}

@test "ssh config: Host=x counts as a Host line" {
  printf 'Host=x\n    User y\nInclude athome/*.conf\n' >"$DEST/.ssh/config"
  [ "$(through .ssh/config)" = "$SSH_HOOK
Host=x
    User y" ]
}

@test "zshrc: a user block inside the old body keeps its done, and the result parses" {
  command -v zsh >/dev/null || skip "zsh not installed"
  old_zshrc | awk 'NR == 50 { print "for d in a b; do"; print "  echo $d"; print "done" } { print }' >"$DEST/.zshrc"
  through .zshrc >"$TMP/zshrc"
  [ "$(cat "$TMP/zshrc")" = "$ZSH_HOOK
for d in a b; do
  echo \$d
done" ]
  zsh -n "$TMP/zshrc"
}

@test "ssh config: a user Host block whose options match athome's keeps all of them" {
  old_ssh | awk '/^Include config.local$/ { print; print "Host gitlab.com"; print "    User git"; print "    IdentitiesOnly yes"; print "    ServerAliveInterval 60"; next } { print }' >"$DEST/.ssh/config"
  [ "$(through .ssh/config)" = "$SSH_HOOK
Host gitlab.com
    User git
    IdentitiesOnly yes
    ServerAliveInterval 60" ]
}

@test "zshrc and ssh config: every old athome body, for every machine type, migrates to the hook alone" {
  for t in personal work remote; do
    for v in "$ROOT"/home/.chezmoitemplates/legacy/ssh-config/v*; do
      chezmoi --source "$ROOT/home" --config "$TMP/$t.toml" execute-template <"$v" >"$DEST/.ssh/config"
      [ "$(through .ssh/config "$t")" = "$SSH_HOOK" ]
    done
  done
  for v in "$ROOT"/home/.chezmoitemplates/legacy/zshrc/v*; do
    cp "$v" "$DEST/.zshrc"
    [ "$(through .zshrc)" = "$ZSH_HOOK" ]
  done
}

@test "git: a later git config --global user.email does not beat the per-directory identity" {
  mkdir -p "$DEST/.config/git" "$DEST/work/r" "$DEST/other"
  printf '[data]\n  machineType = "personal"\n  name_personal = "P"\n  email_personal = "p@example.org"\n' >"$TMP/g.toml"
  chezmoi --source "$ROOT/home" --config "$TMP/g.toml" execute-template <"$ROOT/home/dot_config/git/config.tmpl" >"$DEST/.config/git/config"
  printf '[user]\n\temail = w@example.org\n' >"$DEST/.gitconfig-work"
  rm -f "$DEST/.gitconfig"; through .gitconfig >"$TMP/gc" && cp "$TMP/gc" "$DEST/.gitconfig"
  g() { HOME="$DEST" XDG_CONFIG_HOME="$DEST/.config" GIT_CONFIG_NOSYSTEM=1 git "$@"; }
  g -C "$DEST/work/r" init -q
  g config --global user.email me@example.org
  [ "$(g -C "$DEST/work/r" config user.email)" = "w@example.org" ]
  [ "$(g -C "$DEST/other" config user.email)" = "me@example.org" ]
}
