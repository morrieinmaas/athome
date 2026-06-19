# shellcheck shell=bash
# Shared native-package-manager helper — `source` me.
#
# pm_install <pkg...>: install native packages via the OS's package manager,
# mapping the few per-OS name differences (e.g. gh → github-cli on Arch). Used
# for the handful of native deps bootstrap needs EARLY (gh, rbw); the full
# package set is installed by run_onchange_02 on `mise run apply`.
pm_install() {
  if [ "$(uname -s)" = Darwin ]; then
    nb install "$@"
  elif command -v dnf >/dev/null 2>&1 && ! command -v pacman >/dev/null 2>&1; then
    sudo dnf install -y "$@"
  else
    local -a pkgs=() ; local p
    for p in "$@"; do
      case "$p" in
        gh) pkgs+=(github-cli) ;;   # Arch ships gh as `github-cli`
        *)  pkgs+=("$p") ;;
      esac
    done
    yay -S --needed --noconfirm --answerdiff None --answerclean None "${pkgs[@]}"
  fi
}
