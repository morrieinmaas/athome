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
# ifconfig 7ms, netbird status 142ms, system_profiler SPAirPortDataType 6020ms,
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
    [ -n "$svc" ] && row dns "$(networksetup -getdnsservers "$svc" 2>/dev/null | tr '\n' ' ')"
  fi
  row "public ip" "$(curl -4 -s --max-time 5 https://1.1.1.1/cdn-cgi/trace 2>/dev/null | sed -n 's/^ip=//p')"
}

section_wg() {
  echo "WIREGUARD (nord0)"
  if [ -e /var/run/wireguard/nord0.name ]; then
    row state up
    ifconfig 2>/dev/null | awk '/^utun/{i=substr($1,1,length($1)-1)} /inet 10\.5\./{printf "  %-12s %s (%s)\n","address",$2,i}'
    # The user-owned copy is readable; /etc/wireguard/nord0.conf is root 600.
    conf="$HOME/.secrets/nordvpn/nord0.conf"
    if [ -r "$conf" ]; then
      row server   "$(sed -n 's/^# Server: //p' "$conf")"
      row endpoint "$(sed -n 's/^Endpoint *= *//p' "$conf")"
    fi
  else
    row state "DOWN"
    row "bring up" "sudo PATH=/opt/nanobrew/prefix/bin:\$PATH wg-quick up nord0"
  fi
}

section_nb() {
  echo "NETBIRD"
  s=$(netbird status -d 2>/dev/null)
  if [ -z "$s" ]; then row state "daemon not responding"; return; fi
  # State this machine's own mesh membership first. Without it the peer count
  # reads as "NetBird is disconnected", when it means the far end is offline.
  case "$s" in
    *"Management: Connected"*) row daemon "connected to the mesh" ;;
    *)                         row daemon "NOT connected" ;;
  esac
  row address "$(printf '%s\n' "$s" | sed -n 's/^NetBird IP: //p' | head -1)"
  row fqdn    "$(printf '%s\n' "$s" | sed -n 's/^FQDN: //p' | head -1)"
  # "Peers count: 0/1 Connected" is NetBird's own wording and reads as a
  # contradiction next to a 0. Spell it out instead.
  pc=$(printf '%s\n' "$s" | sed -n 's/.*Peers count: \([0-9]*\)\/\([0-9]*\).*/\1 of \2/p' | head -1)
  row peers "${pc:-unknown} connected"
  # Per-peer rows are only meaningful once a peer is actually connected; with
  # none they are all "-", which looks like something is wrong rather than idle.
  ct=$(printf '%s\n' "$s" | sed -n 's/^ *Connection type: //p' | head -1)
  case "$ct" in ""|"-") ;; *) row "conn type" "$ct"; row handshake "$(printf '%s\n' "$s" | sed -n 's/^ *Last WireGuard handshake: //p' | head -1)" ;; esac
}

# tmux hands back the full range name, "user|net", so match on a substring.
case "${1:-}" in
  *net_vpn*) body=$( { section_wg; echo; section_nb; echo; section_link; } ) ;;
  *)         body=$( { section_link; echo; section_wg; echo; section_nb; } ) ;;
esac

# No --border here on purpose: tmux draws it via `display-popup -b rounded -T`.
# Having both meant fzf's border was clipped at the popup edge, losing the
# top-left corner and the label. Matches pet-pick.sh, which also lets the popup
# own the frame.
printf '%s\n' "$body" | fzf \
  --reverse --height=100% --no-sort --no-separator --no-scrollbar \
  --prompt='filter ▸ ' --header='esc to close' \
  --color="$COLORS" >/dev/null
