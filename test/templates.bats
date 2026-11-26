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
  perl -pe 's/\.chezmoi\.hostname/"fixture"/g' "$ROOT/home/private_dot_ssh/private_athome/private_base.conf.tmpl" |
    chezmoi --source "$ROOT/home" --config "$TMP/$1.toml" execute-template
}

@test "ssh config: remote pins GitHub's host keys for every ssh on the box" {
  out="$(render_ssh_config remote)"
  block="$(printf '%s\n' "$out" | awk '/^Host github.com$/{f=1;next} f&&/^Host /{exit} f')"
  [[ "$block" == *"UserKnownHostsFile ~/.ssh/known_hosts ~/.ssh/athome_github_known_hosts"* ]]
  [[ "$block" == *"StrictHostKeyChecking yes"* ]]
}

# test/fixtures/ssh-config is the whole ~/.ssh/config athome rendered before
# base.conf existed; base.conf carries the same settings under a new header.
@test "ssh config: personal and work base.conf is the old file under a new header, one comment updated" {
  tail -n +3 "$ROOT/test/fixtures/ssh-config" >"$TMP/old-body"
  for t in personal work; do
    render_ssh_config "$t" | tail -n +8 |
      perl -pe 's{see ~/.config/git/config includeIf}{see ~/.gitconfig includeIf}' >"$TMP/ssh-config.$t"
    cmp "$TMP/ssh-config.$t" "$TMP/old-body"
  done
}

# render_with_keys <personal|work|remote> <template under home/> renders it with
# a HOME that holds both an id_ed25519.pub and the (pinned) fixture host's key.
render_with_keys() {
  mkdir -p "$TMP/keyhome/.ssh"
  printf 'ssh-ed25519 AAAAIDKEY id\n' >"$TMP/keyhome/.ssh/id_ed25519.pub"
  printf 'ssh-ed25519 AAAABOXKEY box\n' >"$TMP/keyhome/.ssh/fixture_ed25519.pub"
  { cat "$TMP/$1.toml"; printf '  name_personal = "P"\n  email_personal = "p@example.org"\n  name_work = ""\n  email_work = ""\n  name_sidebiz = ""\n  email_sidebiz = ""\n'; } >"$TMP/keys-$1.toml"
  perl -pe 's/\.chezmoi\.hostname/"fixture"/g' "$ROOT/home/$2" |
    HOME="$TMP/keyhome" chezmoi --source "$ROOT/home" --config "$TMP/keys-$1.toml" execute-template
}

@test "ssh config: remote offers only the box key to GitHub, a laptop keeps both" {
  block="$(render_ssh_config remote | awk '/^Host github.com$/{f=1;next} f&&/^Host /{exit} f')"
  [[ "$block" == *"IdentityFile ~/.ssh/fixture_ed25519"* ]]
  [[ "$block" == *"IdentitiesOnly yes"* ]]
  [[ "$block" != *"IdentityFile ~/.ssh/id_ed25519"* ]]
  block="$(render_ssh_config personal | awk '/^Host github.com$/{f=1;next} f&&/^Host /{exit} f')"
  [[ "$block" == *"IdentityFile ~/.ssh/id_ed25519"* ]]
  [[ "$block" == *"IdentityFile ~/.ssh/fixture_ed25519"* ]]
}

@test "gitconfig: remote signs with the box key even when id_ed25519 exists, a laptop prefers id_ed25519" {
  for f in dot_gitconfig-work.tmpl dot_gitconfig-personal.tmpl dot_gitconfig-sidebiz.tmpl; do
    [[ "$(render_with_keys remote "$f")" == *"signingkey = $TMP/keyhome/.ssh/fixture_ed25519.pub"* ]]
    [[ "$(render_with_keys personal "$f")" == *"signingkey = $TMP/keyhome/.ssh/id_ed25519.pub"* ]]
    [[ "$(render_with_keys work "$f")" == *"signingkey = $TMP/keyhome/.ssh/id_ed25519.pub"* ]]
  done
}

@test "allowed_signers: remote lists the box key, a laptop lists id_ed25519" {
  out="$(render_with_keys remote dot_config/git/allowed_signers.tmpl)"
  [[ "$out" == *"AAAABOXKEY"* && "$out" != *"AAAAIDKEY"* ]]
  out="$(render_with_keys personal dot_config/git/allowed_signers.tmpl)"
  [[ "$out" == *"AAAAIDKEY"* && "$out" != *"AAAABOXKEY"* ]]
}

@test "vpn client: remote keeps the mesh and renders no commercial VPN, a laptop keeps both" {
  s=.chezmoiscripts/run_once_08-setup-vpn-client.sh.tmpl
  printf '  nordvpn_country = ""\n  netbird_management_url = ""\n' | tee -a "$TMP/personal.toml" >>"$TMP/remote.toml"
  for os in linux darwin; do
    render remote "$s" "$os" >"$TMP/vpn.$os.sh"
    bash -n "$TMP/vpn.$os.sh"
    grep -q 'sudo netbird up' "$TMP/vpn.$os.sh"
    if grep -v '^#' "$TMP/vpn.$os.sh" | grep -qi 'nordvpn\|protonvpn'; then return 1; fi
    render personal "$s" "$os" | grep -qi nordvpn
  done
  render remote "$s" linux | grep -q 'netbird@main'
}

@test "run_onchange_02: remote skips the NordVPN packages and repo, a laptop installs them" {
  s=.chezmoiscripts/run_onchange_02-install-packages.sh.tmpl
  render remote "$s" linux >"$TMP/remote.sh"
  if grep -v '^ *#' "$TMP/remote.sh" | grep -qi nordvpn; then return 1; fi
  render personal "$s" linux >"$TMP/personal.sh"
  grep -q 'install_one yay "nordvpn-bin"' "$TMP/personal.sh"
  grep -q 'install_one dnf "nordvpn"' "$TMP/personal.sh"
}

@test "laptop scripts: personal and work render byte for byte what origin/main rendered" {
  # test/fixtures/laptop holds each script as origin/main rendered it for a laptop on
  # that OS (personal and work were identical). A changed rendering changes chezmoi's
  # hash of a run_once script, so every laptop would run it again. The packages.yaml
  # hash line of run_onchange_02 differs by design (the file got gui flags), so it is left out.
  for t in personal work; do
    printf '  nordvpn_country = ""\n  netbird_management_url = ""\n  mesh_provider = "meshnet"\n' >>"$TMP/$t.toml"
  done
  for f in run_once_08-setup-vpn-client run_once_09-setup-caps-to-ctrl run_once_11-setup-niri-noctalia \
    run_once_15-setup-wifi-backend run_once_after_20-setup-zen run_once_after_23-enable-gpaste \
    run_onchange_02-install-packages run_onchange_after_24-enable-canopy-tmux-service \
    run_onchange_after_25-desktop-look; do
    for t in personal work; do
      for os in linux darwin; do
        render "$t" ".chezmoiscripts/$f.sh.tmpl" "$os" | perl -ne 'print unless /^#   packages\.yaml hash = /' >"$TMP/out"
        if ! cmp -s "$TMP/out" "$ROOT/test/fixtures/laptop/$f.$os"; then
          echo "$f differs for $t on $os" >&2
          return 1
        fi
      done
    done
  done
}

@test "laptop vpn script: netbird with country and url set renders what origin/main rendered" {
  f=run_once_08-setup-vpn-client
  for t in personal work; do
    printf '  nordvpn_country = "Netherlands"\n  netbird_management_url = "https://nb.example"\n  mesh_provider = "netbird"\n' >>"$TMP/$t.toml"
    for os in linux darwin; do
      render "$t" ".chezmoiscripts/$f.sh.tmpl" "$os" >"$TMP/out"
      if ! cmp -s "$TMP/out" "$ROOT/test/fixtures/laptop/$f.netbird.$t.$os"; then
        echo "$f netbird differs for $t on $os" >&2
        return 1
      fi
    done
  done
}

@test "vpn client: a remote box with mesh_provider meshnet still gets netbird" {
  s=.chezmoiscripts/run_once_08-setup-vpn-client.sh.tmpl
  printf '  nordvpn_country = ""\n  netbird_management_url = ""\n  mesh_provider = "meshnet"\n' >>"$TMP/remote.toml"
  for os in linux darwin; do
    render remote "$s" "$os" >"$TMP/vpn.$os.sh"
    grep -q 'sudo netbird up' "$TMP/vpn.$os.sh"
    if grep -q 'skipping NetBird' "$TMP/vpn.$os.sh"; then return 1; fi
  done
}

# canopy_remote_home makes a HOME whose canopy-remote stub logs its arguments and
# writes the two files from them, and whose grofe stub logs each call and answers
# reload with $GROFE_RELOAD_OUT and $GROFE_RELOAD_CODE.
canopy_remote_home() {
  mkdir -p "$TMP/home/.local/share/canopy/bin" "$TMP/home/.local/lib/grofe"
  cat >"$TMP/home/.local/share/canopy/bin/canopy-remote" <<STUB
#!/bin/sh
printf '%s\n' "\$*" >>"$TMP/remote.calls"
mkdir -p "\$HOME/.local/state/canopy" "\$HOME/.config/grofe/conf.d"
printf '%s\n' "\$*" >"\$HOME/.local/state/canopy/25-remote.conf"
printf '%s\n' "\$*" >"\$HOME/.config/grofe/conf.d/25-remote.toml"
STUB
  cat >"$TMP/home/.local/lib/grofe/grofe" <<STUB
#!/bin/sh
printf '%s\n' "\$*" >>"$TMP/grofe.calls"
printf '%s' "\${GROFE_RELOAD_OUT:-}" >&2
exit "\${GROFE_RELOAD_CODE:-0}"
STUB
  chmod +x "$TMP/home/.local/share/canopy/bin/canopy-remote" "$TMP/home/.local/lib/grofe/grofe"
}

# run_canopy_remote <rendered script> runs it with HOME at the fixture, never the real one.
run_canopy_remote() {
  run env -u CANOPY_STATE -u XDG_STATE_HOME -u XDG_CONFIG_HOME HOME="$TMP/home" bash "$1"
}

@test "canopy remote: renders only on a remote machine, with no OS branch" {
  s=.chezmoiscripts/run_after_31-canopy-remote.sh.tmpl
  for t in personal work; do
    [ -z "$(render "$t" "$s" | tr -d '[:space:]')" ]
  done
  if grep -q 'chezmoi\.os' "$ROOT/home/$s"; then return 1; fi
  render remote "$s" >"$TMP/cr.sh"
  bash -n "$TMP/cr.sh"
  shellcheck -S warning -x "$TMP/cr.sh" 2>/dev/null || ! command -v shellcheck >/dev/null
}

@test "remote-box script: a failing sshd -t puts back the drop-in that was there" {
  render remote .chezmoiscripts/run_onchange_after_30-remote-box.sh.tmpl linux >"$TMP/box.sh"
  mkdir "$TMP/home" "$TMP/stubs"
  # sudo cat answers with an old drop-in; sshd -t fails on the new one and passes
  # on the old one (or always fails with SSHD_FAIL); tee logs what it writes.
  cat >"$TMP/stubs/sudo" <<STUB
#!/bin/sh
case "\$1" in
  sshd) n=\$((\$(cat "$TMP/sshd.n" 2>/dev/null || echo 0) + 1)); echo \$n >"$TMP/sshd.n"
    [ -z "\${SSHD_FAIL:-}" ] && [ \$n -ge 2 ] ;;
  mkdir) exit 0 ;;
  cat) printf 'PermitRootLogin no\n'; exit 0 ;;
  tee) { printf 'tee:'; cat; } >>"$TMP/calls"; exit 0 ;;
  *) printf '%s\n' "\$*" >>"$TMP/calls"; exit 0 ;;
esac
STUB
  chmod +x "$TMP/stubs/sudo"
  printf '#!/bin/sh\nexit 0\n' >"$TMP/stubs/systemctl"
  chmod +x "$TMP/stubs/systemctl"
  HOME="$TMP/home" PATH="$TMP/stubs:$PATH" run bash "$TMP/box.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"put the previous one back"* ]]
  # the new drop-in, then the old one written back last
  [ "$(grep -c '^tee:' "$TMP/calls")" = 2 ]
  [ "$(grep '^tee:' "$TMP/calls" | tail -n 1)" = 'tee:PermitRootLogin no' ]
  if grep -q '^rm -f' "$TMP/calls"; then return 1; fi
  # an old drop-in that fails too is removed, never left in place
  rm -f "$TMP/calls" "$TMP/sshd.n"
  HOME="$TMP/home" PATH="$TMP/stubs:$PATH" SSHD_FAIL=1 run bash "$TMP/box.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"removed it again"* ]]
  grep -q '^rm -f /etc/ssh/sshd_config.d/10-athome.conf' "$TMP/calls"
}

@test "canopy-theme.zsh: a pane inside grofe or tmux follows this machine's theme even over ssh" {
  command -v zsh >/dev/null || skip "zsh not installed"
  mkdir -p "$TMP/home/.config/themes"
  printf 'gruvbox\n' >"$TMP/home/.config/themes/active"
  for mux in TMUX=/tmp/x,1,0 GROFE=1; do
    run env HOME="$TMP/home" SSH_CONNECTION="10.0.0.1 1 10.0.0.2 22" CANOPY_THEME=everforest "$mux" \
      zsh -f -c 'source "$1"; for f in $precmd_functions; do $f; done; print -r -- "$CANOPY_THEME"' _ "$ROOT/home/dot_zsh/canopy-theme.zsh"
    [ "$output" = gruvbox ]
  done
}

@test "canopy remote: set gets machine, scope and the context colour, then a running grofe reloads" {
  render remote .chezmoiscripts/run_after_31-canopy-remote.sh.tmpl >"$TMP/cr.sh"
  canopy_remote_home
  run_canopy_remote "$TMP/cr.sh"
  [ "$status" -eq 0 ]
  [ "$(cat "$TMP/remote.calls")" = "set --machine acme-dev --scope work/acme --color yellow" ]
  [ "$(cat "$TMP/grofe.calls")" = "reload" ]
}

@test "canopy remote: a second apply with the same values sets again but does not reload" {
  render remote .chezmoiscripts/run_after_31-canopy-remote.sh.tmpl >"$TMP/cr.sh"
  canopy_remote_home
  run_canopy_remote "$TMP/cr.sh"
  run_canopy_remote "$TMP/cr.sh"
  [ "$status" -eq 0 ]
  [ "$(wc -l <"$TMP/remote.calls" | tr -d ' ')" = 2 ]
  [ "$(cat "$TMP/grofe.calls")" = "reload" ]
}

@test "canopy remote: no grofe server running is silent, another reload failure is a warning" {
  render remote .chezmoiscripts/run_after_31-canopy-remote.sh.tmpl >"$TMP/cr.sh"
  canopy_remote_home
  GROFE_RELOAD_CODE=1 GROFE_RELOAD_OUT="grofe: no server running on /tmp/x" run_canopy_remote "$TMP/cr.sh"
  [ "$status" -eq 0 ]
  [ -z "$output" ]
  rm "$TMP/home/.local/state/canopy/25-remote.conf"
  GROFE_RELOAD_CODE=1 GROFE_RELOAD_OUT="bad config" run_canopy_remote "$TMP/cr.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"grofe reload failed (bad config)"* ]]
}

@test "canopy remote: a canopy without canopy remote, or without grofe, still passes" {
  render remote .chezmoiscripts/run_after_31-canopy-remote.sh.tmpl >"$TMP/cr.sh"
  mkdir "$TMP/home"
  run_canopy_remote "$TMP/cr.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"no \`canopy remote\`"* ]]
  canopy_remote_home
  rm "$TMP/home/.local/lib/grofe/grofe"
  run_canopy_remote "$TMP/cr.sh"
  [ "$status" -eq 0 ]
  [ -s "$TMP/remote.calls" ]
  [ ! -e "$TMP/grofe.calls" ]
}

@test "canopy remote: a sidebiz box gets magenta" {
  printf '[data]\n  machineType = "remote"\n  machineName = "box"\n  scope = "sidebiz/x"\n' >"$TMP/sb.toml"
  render sb .chezmoiscripts/run_after_31-canopy-remote.sh.tmpl >"$TMP/cr.sh"
  canopy_remote_home
  run_canopy_remote "$TMP/cr.sh"
  [ "$(cat "$TMP/remote.calls")" = "set --machine box --scope sidebiz/x --color magenta" ]
}

@test "remote-box script: sshd accepts CANOPY_THEME and no other variable" {
  render remote .chezmoiscripts/run_onchange_after_30-remote-box.sh.tmpl linux >"$TMP/box.sh"
  want="$(awk '/^want="/{f=1} f{print} f&&/"$/&&!/^want="$/{exit}' "$TMP/box.sh")"
  [ "$want" = 'want="PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin no
AcceptEnv CANOPY_THEME"' ]
}

@test "athome.zsh: every machine type sources canopy-theme.zsh and drift-notice.zsh" {
  for t in personal work remote; do
    render "$t" dot_zsh/athome.zsh | grep -q 'ssh-nudge.zsh canopy-theme.zsh drift-notice.zsh; do'
  done
}

@test "canopy-theme.zsh: each prompt exports the active theme's name, and nothing without one" {
  command -v zsh >/dev/null || skip "zsh not installed"
  mkdir -p "$TMP/home/.config/themes"
  run env -u CANOPY_THEME -u SSH_CONNECTION HOME="$TMP/home" zsh -f -c '
    source "$1"
    for f in $precmd_functions; do $f; done
    print -r -- "unset:${CANOPY_THEME-none}"
    print everforest >~/.config/themes/active
    for f in $precmd_functions; do $f; done
    print -r -- "set:$CANOPY_THEME"
    print gruvbox >~/.config/themes/active
    for f in $precmd_functions; do $f; done
    print -r -- "set:$CANOPY_THEME"
    rm ~/.config/themes/active
    for f in $precmd_functions; do $f; done
    print -r -- "kept:$CANOPY_THEME"
  ' _ "$ROOT/home/dot_zsh/canopy-theme.zsh"
  [ "$status" -eq 0 ]
  [ "$output" = $'unset:none\nset:everforest\nset:gruvbox\nkept:gruvbox' ]
}

@test "theme: a remote machine keeps the theme command and the themes dir" {
  mkdir -p "$TMP/home" "$TMP/dest"
  for t in remote personal; do
    HOME="$TMP/home" chezmoi --source "$ROOT/home" --config "$TMP/$t.toml" --destination "$TMP/dest" \
      --persistent-state "$TMP/state.boltdb" managed --include files >"$TMP/managed.$t"
    for f in .local/bin/theme .config/themes/registry.sh .config/themes/apply-tmux.sh; do
      grep -qx "$f" "$TMP/managed.$t"
    done
  done
}

@test "canopy-theme.zsh: an ssh login keeps the CANOPY_THEME the laptop sent" {
  command -v zsh >/dev/null || skip "zsh not installed"
  mkdir -p "$TMP/home/.config/themes"
  print_theme='source "$1"; for f in $precmd_functions; do $f; done; print -r -- "$CANOPY_THEME"'
  printf 'gruvbox\n' >"$TMP/home/.config/themes/active"
  run env HOME="$TMP/home" SSH_CONNECTION="10.0.0.1 1 10.0.0.2 22" CANOPY_THEME=everforest \
    zsh -f -c "$print_theme" _ "$ROOT/home/dot_zsh/canopy-theme.zsh"
  [ "$output" = everforest ]
  # over ssh without one sent, this machine's own theme is exported
  run env -u CANOPY_THEME HOME="$TMP/home" SSH_CONNECTION="10.0.0.1 1 10.0.0.2 22" \
    zsh -f -c "$print_theme" _ "$ROOT/home/dot_zsh/canopy-theme.zsh"
  [ "$output" = gruvbox ]
}

@test "canopy remote: a failing set is a warning, the apply goes on and grofe is left alone" {
  render remote .chezmoiscripts/run_after_31-canopy-remote.sh.tmpl >"$TMP/cr.sh"
  canopy_remote_home
  printf '#!/bin/sh\nexit 1\n' >"$TMP/home/.local/share/canopy/bin/canopy-remote"
  run_canopy_remote "$TMP/cr.sh"
  [ "$status" -eq 0 ]
  [[ "$output" == *"canopy remote set failed"* ]]
  [ ! -e "$TMP/grofe.calls" ]
}
