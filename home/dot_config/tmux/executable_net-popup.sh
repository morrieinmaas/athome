#!/usr/bin/env bash
# Detail view for the status-bar network pills (net.sh). Opened by clicking the
# pills, or with `prefix W`.
#
# Rendered through fzf rather than plain text for two reasons. It matches the
# other popups in this config (pet-pick, theme), and more importantly a plain
# `read -n1` hold-open swallows the mouse-up event that follows the click that
# opened it, so the popup vanished instantly and the click looked dead.
#
# This is where the expensive calls live: net.sh must stay fast because it runs
# every status-interval, whereas this runs only on demand. Measured here:
# ifconfig 7ms, networksetup ~160ms, system_profiler SPAirPortDataType 6020ms,
# which is why system_profiler is not used at all.
#
# display-popup runs in the tmux SERVER environment, which never sources
# ~/.zshenv, so PATH is set explicitly (same reason cockpit and pet-pick do it).
export PATH="$HOME/.local/share/mise/shims:/opt/nanobrew/prefix/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"

. "$HOME/.config/tmux/fzf-theme.sh"

row() { printf '  %-12s %s\n' "$1" "$2"; }

section_link() {
  echo "LINK"
  # Same idiom as net.sh: macOS service priority order, not the routing table.
  # A VPN holding a real default route otherwise reports the tunnel as the link.
  lan_if=""
  while read -r _dev; do
    case "$_dev" in en*) ;; *) continue ;; esac
    if ipconfig getifaddr "$_dev" >/dev/null 2>&1; then lan_if="$_dev"; break; fi
  done < <(networksetup -listnetworkserviceorder 2>/dev/null \
             | sed -n 's/.*Device: \([^)]*\)).*/\1/p')
  gw=$(ipconfig getoption "${lan_if:-en0}" router 2>/dev/null)
  row interface "${lan_if:-none}"
  if [ -n "$lan_if" ]; then
    row address "$(ifconfig "$lan_if" 2>/dev/null | awk '/inet /{print $2; exit}')"
    row router  "${gw:-unknown}"
  fi
  # No SSID row: macOS redacts the network name and BSSID from callers without
  # Location Services permission, and enabling that for a terminal is not worth
  # a cosmetic label. The DNS row matters more here anyway, because wg-quick
  # rewrites the interface's DNS while the tunnel is up.
  if [ "$(uname)" = Darwin ] && [ -n "$lan_if" ]; then
    svc=$(networksetup -listnetworkserviceorder 2>/dev/null \
      | awk -v d="$lan_if" '/^\([0-9]+\)/{s=substr($0,index($0,") ")+2)} $0 ~ "Device: "d"\\)" {print s; exit}')
    if [ -n "$svc" ]; then
      # networksetup answers with a whole sentence when nothing is pinned;
      # "DHCP" is the useful word.
      d=$(networksetup -getdnsservers "$svc" 2>/dev/null | tr '\n' ' ')
      case "$d" in *"aren't any"*|"") d="DHCP" ;; esac
      row dns "$d"
    fi
  fi
  # No public-IP row here: it belongs with VPN egress, and having it in both
  # sections meant two curl calls and ~1s of avoidable latency in the popup.
}

section_vpn() {
  echo "VPN EGRESS"
  # Provider-agnostic: ask whether the default route is a tunnel, rather than
  # interrogating a specific vendor's CLI. Works for NordVPN, Proton, Mullvad
  # and a hand-rolled wg-quick tunnel alike.
  vpn_if=$(route -n get default 2>/dev/null | awk '/interface:/{print $2; exit}')
  case "$vpn_if" in
    utun*|wg*|tun*|nordlynx)
      row state "tunnelled via $vpn_if"
      row address "$(ifconfig "$vpn_if" 2>/dev/null | awk '/inet /{print $2; exit}')"
      ;;
    *)
      row state "direct (no VPN)"
      row via   "${vpn_if:-unknown}"
      ;;
  esac
  row "public ip" "$(curl -4 -s --max-time 5 https://1.1.1.1/cdn-cgi/trace 2>/dev/null | sed -n 's/^ip=//p')"
}

section_mesh() {
  echo "MESH"
  # NordVPN Meshnet, Tailscale and NetBird all allocate from the RFC 6598
  # shared range 100.64.0.0/10, so an address there means this machine is on a
  # mesh, whichever product is providing it.
  mesh=$(ifconfig 2>/dev/null \
    | awk '/inet 100\.(6[4-9]|[7-9][0-9]|1[01][0-9]|12[0-7])\./{print $2; exit}')
  if [ -n "$mesh" ]; then
    row address "$mesh"
    mesh_if=$(ifconfig 2>/dev/null | awk '/^[a-z]/{i=substr($1,1,length($1)-1)} /inet 100\.(6[4-9]|[7-9][0-9]|1[01][0-9]|12[0-7])\./{print i; exit}')
    row interface "${mesh_if:-unknown}"
  else
    row state "not on a mesh"
    row hint  "NordVPN app > Meshnet, or: netbird up"
  fi
}

# tmux hands back the full range name, "user|net", so match on a substring.
case "${1:-}" in
  *net_vpn*) body=$( { section_vpn; echo; section_mesh; echo; section_link; } ) ;;
  *)         body=$( { section_link; echo; section_vpn; echo; section_mesh; } ) ;;
esac

# No --border here on purpose: tmux draws it via `display-popup -b rounded -T`.
# Having both meant fzf's border was clipped at the popup edge, losing the
# top-left corner and the label. Matches pet-pick.sh, which also lets the popup
# own the frame.
printf '%s\n' "$body" | fzf \
  --reverse --height=100% --no-sort --no-separator --no-scrollbar \
  --prompt='filter ▸ ' --header='esc to close' \
  --color="$COLORS" >/dev/null
