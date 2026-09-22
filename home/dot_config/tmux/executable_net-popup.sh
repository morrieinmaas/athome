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
  gw=$(route -n get default 2>/dev/null | awk '/gateway:/{print $2; exit}')
  lan_if=$(route -n get "${gw:-1.1.1.1}" 2>/dev/null | awk '/interface:/{print $2; exit}')
  wifi_dev=$(networksetup -listallhardwareports 2>/dev/null \
    | awk '/Hardware Port: Wi-Fi/{getline; print $2; exit}')
  row interface "${lan_if:-none}"
  if [ -n "$lan_if" ]; then
    row address "$(ifconfig "$lan_if" 2>/dev/null | awk '/inet /{print $2; exit}')"
    row router  "${gw:-unknown}"
  fi
  if [ "$(uname)" = Darwin ]; then
    # networksetup, NOT system_profiler: the latter costs ~6s and both return
    # nothing useful without Location Services permission for the terminal.
    ssid=$(networksetup -getairportnetwork "${wifi_dev:-en0}" 2>/dev/null \
      | sed -n 's/^Current Wi-Fi Network: //p')
    case "$ssid" in
      ""|*"<redacted>"*) row ssid "hidden (grant the terminal Location Services)" ;;
      *)                 row ssid "$ssid" ;;
    esac
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
  printf '%s\n' "$s" | sed -n \
    -e 's/^NetBird IP: /  address      /p' \
    -e 's/^FQDN: /  fqdn         /p' \
    -e 's/^Peers count: /  peers        /p' \
    -e 's/^ *Connection type: /  conn type    /p' \
    -e 's/^ *Last WireGuard handshake: /  handshake    /p' \
    | head -8
}

# tmux hands back the full range name, "user|net", so match on a substring.
case "${1:-}" in
  *net_vpn*) body=$( { section_wg; echo; section_nb; echo; section_link; } ) ;;
  *)         body=$( { section_link; echo; section_wg; echo; section_nb; } ) ;;
esac

printf '%s\n' "$body" | fzf \
  --reverse --height=100% --no-sort --no-separator --no-scrollbar \
  --border=rounded --border-label=' network ' --border-label-pos=3 \
  --prompt='filter ▸ ' --header='esc to close' \
  --color="$COLORS" >/dev/null
