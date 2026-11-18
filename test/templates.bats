#!/usr/bin/env bats
# Remote machine support in the chezmoi templates. Each template is rendered with
# `chezmoi execute-template --source home --config <temp toml>`, never applied.
# Scripts that are run do so with HOME set to a temp dir.

bats_require_minimum_version 1.5.0

setup() {
  unset TMUX GROFE ATHOME_CI
  ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  command -v chezmoi >/dev/null || skip "chezmoi not installed"
  TMP="$BATS_TEST_TMPDIR"
  for t in personal work; do
    printf '[data]\n  machineType = "%s"\n' "$t" >"$TMP/$t.toml"
  done
  cat >"$TMP/remote.toml" <<'TOML'
[data]
  machineType = "remote"
  machineName = "acme-dev"
  scope       = "work/acme"
  timezone    = "Europe/Amsterdam"
  locale      = "en_US.UTF-8"
TOML
}

# render <personal|work|remote> <template path under home/> [os]
# With an os, `.chezmoi.os` is pinned to it so the other OS's branch renders too.
render() {
  local cfg="$TMP/$1.toml" tmpl="$ROOT/home/$2"
  if [ -n "${3:-}" ]; then
    perl -pe "s/\.chezmoi\.os/\"$3\"/g" "$tmpl" |
      chezmoi --source "$ROOT/home" --config "$cfg" execute-template
  else
    chezmoi --source "$ROOT/home" --config "$cfg" execute-template <"$tmpl"
  fi
}

# gui_names prints the name of every `gui: true` entry in packages.yaml.
gui_names() {
  perl -ne 'print "$1\n" if /\{name:\s*([^,\s]+),\s*gui:\s*true\}/' "$ROOT/home/.chezmoidata/packages.yaml"
}

@test "starship: remote replaces the hostname module with machine and scope" {
  out="$(render remote dot_config/starship.toml.tmpl)"
  [[ "$out" == *"[acme-dev · work/acme](bold yellow)"* ]]
  format="$(printf '%s\n' "$out" | awk '/^format = """/{f=1;next} f&&/"""/{exit} f')"
  [[ "$format" != *'$hostname'* ]]
}

@test "starship: personal and work render exactly the old file" {
  for t in personal work; do
    render "$t" dot_config/starship.toml.tmpl >"$TMP/starship.$t"
    cmp "$TMP/starship.$t" "$ROOT/test/fixtures/starship.toml"
  done
}

@test "starship: the remote colour follows the scope's context" {
  sed_cfg="$TMP/sidebiz.toml"
  printf '[data]\n  machineType = "remote"\n  machineName = "box"\n  scope = "sidebiz/x"\n' >"$sed_cfg"
  out="$(chezmoi --source "$ROOT/home" --config "$sed_cfg" execute-template <"$ROOT/home/dot_config/starship.toml.tmpl")"
  [[ "$out" == *"[box · sidebiz/x](bold magenta)"* ]]
}

@test "zshenv: remote exports the three variables" {
  out="$(render remote dot_zshenv.tmpl)"
  [[ "$out" == *"export ATHOME_MACHINE='acme-dev'"* ]]
  [[ "$out" == *"export ATHOME_SCOPE='work/acme'"* ]]
  [[ "$out" == *"export ATHOME_CONTEXT_COLOR='yellow'"* ]]
  printf '%s\n' "$out" >"$TMP/zshenv.remote"
  zsh -n "$TMP/zshenv.remote" 2>/dev/null || ! command -v zsh >/dev/null
}

@test "zshenv: personal and work render exactly the old file" {
  for t in personal work; do
    render "$t" dot_zshenv.tmpl >"$TMP/zshenv.$t"
    cmp "$TMP/zshenv.$t" "$ROOT/test/fixtures/zshenv"
  done
}

@test "environment.d: remote has KEY=value lines, a laptop renders empty" {
  out="$(render remote dot_config/environment.d/athome.conf.tmpl)"
  [[ "$out" == *"ATHOME_MACHINE=acme-dev"* ]]
  [[ "$out" == *"ATHOME_SCOPE=work/acme"* ]]
  [[ "$out" == *"ATHOME_CONTEXT_COLOR=yellow"* ]]
  [ -z "$(render personal dot_config/environment.d/athome.conf.tmpl | tr -d '[:space:]')" ]
}

@test "grofe-restore.service: remote only" {
  out="$(render remote dot_config/systemd/user/grofe-restore.service.tmpl)"
  [[ "$out" == *"Type=forking"* ]]
  [[ "$out" == *"ExecStart=%h/.local/lib/grofe/grofe start-server --restore"* ]]
  [[ "$out" == *"WantedBy=default.target"* ]]
  [ -z "$(render work dot_config/systemd/user/grofe-restore.service.tmpl | tr -d '[:space:]')" ]
}

@test "run_03: remote seeds only ~/<scope>/.envrc with use ctx work" {
  render remote .chezmoiscripts/run_03-setup-project-dirs.sh.tmpl >"$TMP/run03.sh"
  shellcheck -S warning -x "$TMP/run03.sh" 2>/dev/null || ! command -v shellcheck >/dev/null
  mkdir "$TMP/home"
  HOME="$TMP/home" run bash "$TMP/run03.sh"
  [ "$status" -eq 0 ]
  grep -qx 'use ctx work' "$TMP/home/work/acme/.envrc"
  [ "$(cd "$TMP/home" && find . -mindepth 1 | sort | tr '\n' ' ')" = "./work ./work/acme ./work/acme/.envrc " ]
}

@test "run_03: personal still seeds personal and sidebiz" {
  render personal .chezmoiscripts/run_03-setup-project-dirs.sh.tmpl >"$TMP/run03.sh"
  mkdir "$TMP/home"
  HOME="$TMP/home" run bash "$TMP/run03.sh"
  [ "$status" -eq 0 ]
  grep -qx 'use ctx personal' "$TMP/home/personal/.envrc"
  grep -qx 'use ctx sidebiz' "$TMP/home/sidebiz/.envrc"
  [ ! -e "$TMP/home/work" ]
}

@test "run_onchange_02: remote skips every gui entry on linux" {
  [ -n "$(gui_names)" ]
  render remote .chezmoiscripts/run_onchange_02-install-packages.sh.tmpl linux >"$TMP/remote.sh"
  render personal .chezmoiscripts/run_onchange_02-install-packages.sh.tmpl linux >"$TMP/personal.sh"
  bash -n "$TMP/remote.sh"
  for n in $(gui_names); do
    if grep -qE "^ *install_one [a-z]+ \"$n\"\$" "$TMP/remote.sh"; then return 1; fi
  done
  # ...and the laptop keeps them
  grep -q 'install_one yay "niri"' "$TMP/personal.sh"
  grep -q 'install_one yay "git"' "$TMP/remote.sh"
}

@test "run_onchange_02: remote installs no cask or gui entry on macOS" {
  render remote .chezmoiscripts/run_onchange_02-install-packages.sh.tmpl darwin >"$TMP/remote.sh"
  render personal .chezmoiscripts/run_onchange_02-install-packages.sh.tmpl darwin >"$TMP/personal.sh"
  if grep -q '^install_one nb_cask' "$TMP/remote.sh"; then return 1; fi
  grep -q '^install_one nb_cask "ghostty"' "$TMP/personal.sh"
  if grep -q '"defaultbrowser"' "$TMP/remote.sh"; then return 1; fi
  grep -q '"defaultbrowser"' "$TMP/personal.sh"
}

@test "run_onchange_02: personal and work install the same names in the same order" {
  for os in linux darwin; do
    render personal .chezmoiscripts/run_onchange_02-install-packages.sh.tmpl "$os" >"$TMP/p.sh"
    render work .chezmoiscripts/run_onchange_02-install-packages.sh.tmpl "$os" >"$TMP/w.sh"
    diff <(grep '^ *install_one' "$TMP/p.sh") <(grep '^ *install_one' "$TMP/w.sh")
    # ...and both equal the install list from before the gui flags (checked-in fixture)
    diff <(grep '^ *install_one' "$TMP/p.sh") "$ROOT/test/fixtures/install-one-$os.txt"
    # every plain or gui entry of packages.yaml's active lists appears
    [ "$(grep -c '^ *install_one' "$TMP/p.sh")" -gt 40 ]
  done
}

@test "desktop-only scripts render empty on remote and not on personal" {
  for f in run_once_09-setup-caps-to-ctrl run_once_11-setup-niri-noctalia \
    run_once_15-setup-wifi-backend run_once_after_20-setup-zen \
    run_once_after_23-enable-gpaste run_onchange_after_24-enable-canopy-tmux-service \
    run_onchange_after_25-desktop-look; do
    [ -z "$(render remote ".chezmoiscripts/$f.sh.tmpl" linux | tr -d '[:space:]')" ]
    [ -n "$(render personal ".chezmoiscripts/$f.sh.tmpl" linux | tr -d '[:space:]')" ]
  done
}

@test "remote-box script: remote linux only, shellcheck clean, sshd -t guards the drop-in" {
  s=.chezmoiscripts/run_onchange_after_30-remote-box.sh.tmpl
  [ -z "$(render personal "$s" linux | tr -d '[:space:]')" ]
  [ -z "$(render remote "$s" darwin | tr -d '[:space:]')" ]
  render remote "$s" linux >"$TMP/box.sh"
  bash -n "$TMP/box.sh"
  shellcheck -S warning -x "$TMP/box.sh" 2>/dev/null || ! command -v shellcheck >/dev/null
  grep -q 'sshd_config.d/10-athome.conf' "$TMP/box.sh"
  grep -q 'PermitRootLogin no' "$TMP/box.sh"
  grep -q 'loginctl enable-linger' "$TMP/box.sh"
  grep -q "set-timezone 'Europe/Amsterdam'" "$TMP/box.sh"
  grep -q 'enable grofe-restore.service' "$TMP/box.sh"
  # the drop-in is removed again when sshd -t rejects it
  awk '/sshd -t/{f=1} f&&/rm -f "\$dropin"/{ok=1} END{exit !ok}' "$TMP/box.sh"
}

@test "remote-box script: ATHOME_CI skips it without touching anything" {
  render remote .chezmoiscripts/run_onchange_after_30-remote-box.sh.tmpl linux >"$TMP/box.sh"
  mkdir "$TMP/home"
  HOME="$TMP/home" ATHOME_CI=1 run bash "$TMP/box.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *skipped* ]]
}

@test "remote-box script: a failing sshd -t removes the drop-in and keeps going" {
  render remote .chezmoiscripts/run_onchange_after_30-remote-box.sh.tmpl linux >"$TMP/box.sh"
  mkdir "$TMP/home" "$TMP/stubs"
  # sudo/systemctl/loginctl stubs: sshd -t fails, the rest succeeds; tee/cat/rm act on a temp dir.
  cat >"$TMP/stubs/sudo" <<STUB
#!/bin/sh
case "\$1" in
  sshd) exit 1 ;;
  mkdir) exit 0 ;;
  tee) cat >/dev/null; exit 0 ;;
  cat) exit 1 ;;
  rm) printf 'rm %s\n' "\$*" >>"$TMP/calls"; exit 0 ;;
  *) printf '%s\n' "\$*" >>"$TMP/calls"; exit 0 ;;
esac
STUB
  chmod +x "$TMP/stubs/sudo"
  printf '#!/bin/sh\nexit 0\n' >"$TMP/stubs/systemctl"
  chmod +x "$TMP/stubs/systemctl"
  HOME="$TMP/home" PATH="$TMP/stubs:$PATH" run bash "$TMP/box.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"sshd -t rejected"* ]]
  grep -q 'rm -f /etc/ssh/sshd_config.d/10-athome.conf' "$TMP/calls"
  grep -q 'loginctl enable-linger' "$TMP/calls"
}

@test ".chezmoi.toml.tmpl: carries remote keys, leaves them empty on a laptop, never prompts for them" {
  out="$(chezmoi --source "$ROOT/home" --config "$TMP/remote.toml" execute-template --init <"$ROOT/home/.chezmoi.toml.tmpl")"
  [[ "$out" == *'scope        = "work/acme"'* ]]
  [[ "$out" == *'machineName  = "acme-dev"'* ]]
  [[ "$out" == *'timezone     = "Europe/Amsterdam"'* ]]
  [[ "$out" == *'locale       = "en_US.UTF-8"'* ]]
  out="$(chezmoi --source "$ROOT/home" --config "$TMP/personal.toml" execute-template --init <"$ROOT/home/.chezmoi.toml.tmpl")"
  [[ "$out" == *'scope        = ""'* ]]
  [[ "$out" == *'machineName  = ""'* ]]
}

@test "remote-box script: a timezone or locale with a quote renders no timedatectl or localectl call" {
  printf '[data]\n  machineType = "remote"\n  timezone = "Europe/A'"'"'; id"\n  locale = "en_US.UTF-8; id"\n' >"$TMP/bad.toml"
  render bad .chezmoiscripts/run_onchange_after_30-remote-box.sh.tmpl linux >"$TMP/box.sh"
  bash -n "$TMP/box.sh"
  if grep -q 'timedatectl\|localectl\|locale-gen' "$TMP/box.sh"; then return 1; fi
}

@test "remote-box script: a locale with @ reaches perl as text, not as an array" {
  printf '[data]\n  machineType = "remote"\n  locale = "de_DE@euro"\n' >"$TMP/at.toml"
  render at .chezmoiscripts/run_onchange_after_30-remote-box.sh.tmpl linux >"$TMP/box.sh"
  perl -pi -e "s{/etc/locale\\.gen}{$TMP/locale.gen}g" "$TMP/box.sh"
  printf '#de_DE@euro ISO-8859-15\n#de_DE.UTF-8 UTF-8\n#de ISO-8859-1\n' >"$TMP/locale.gen"
  mkdir "$TMP/home" "$TMP/stubs"
  # sudo runs perl and env for real, answers cat with "no", and succeeds at the rest
  cat >"$TMP/stubs/sudo" <<'STUB'
#!/bin/sh
case "$1" in
  env | perl) exec "$@" ;;
  cat) exit 1 ;;
  *) cat >/dev/null 2>&1 </dev/null; exit 0 ;;
esac
STUB
  chmod +x "$TMP/stubs/sudo"
  printf '#!/bin/sh\nexit 0\n' >"$TMP/stubs/systemctl"
  printf '#!/bin/sh\nexit 0\n' >"$TMP/stubs/locale-gen"
  chmod +x "$TMP/stubs/systemctl" "$TMP/stubs/locale-gen"
  HOME="$TMP/home" PATH="$TMP/stubs:$PATH" run bash "$TMP/box.sh"
  [ "$status" -eq 0 ]
  [[ "$output" != *"could not generate the locale"* ]]
  [ "$(sed -n 1p "$TMP/locale.gen")" = "de_DE@euro ISO-8859-15" ]
  [ "$(sed -n 2p "$TMP/locale.gen")" = "#de_DE.UTF-8 UTF-8" ]
  [ "$(sed -n 3p "$TMP/locale.gen")" = "#de ISO-8859-1" ]
}

# render_ssh_config <personal|work|remote> pins the hostname so the output is comparable.
render_ssh_config() {
  perl -pe 's/\.chezmoi\.hostname/"fixture"/g' "$ROOT/home/private_dot_ssh/private_config.tmpl" |
    chezmoi --source "$ROOT/home" --config "$TMP/$1.toml" execute-template
}

@test "ssh config: remote pins GitHub's host keys for every ssh on the box" {
  out="$(render_ssh_config remote)"
  block="$(printf '%s\n' "$out" | awk '/^Host github.com$/{f=1;next} f&&/^Host /{exit} f')"
  [[ "$block" == *"UserKnownHostsFile ~/.ssh/known_hosts ~/.ssh/athome_github_known_hosts"* ]]
  [[ "$block" == *"StrictHostKeyChecking yes"* ]]
}

@test "ssh config: personal and work render exactly the old file" {
  for t in personal work; do
    render_ssh_config "$t" >"$TMP/ssh-config.$t"
    cmp "$TMP/ssh-config.$t" "$ROOT/test/fixtures/ssh-config"
  done
}
