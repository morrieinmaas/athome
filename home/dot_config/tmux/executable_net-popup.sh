#!/usr/bin/env bash
# Detail popup for the status-bar network pills (net.sh). Bound to a click on
# either pill via #[range=user|net_link] / #[range=user|net_vpn].
#
# This is where the expensive calls live. net.sh must stay under a few hundred
# ms because it runs every status-interval; here a second or two is fine because
# it runs only when you ask. $1 is the range name, so the popup can lead with
# whichever pill you clicked.
#
# display-popup/run-shell inherit the tmux server's environment, which has no
# ~/.zshenv, so PATH is set explicitly (same reason cockpit does it).
export PATH="/opt/nanobrew/prefix/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"

hr() { printf '%s\n' "────────────────────────────────────────────"; }

section_link() {
  echo "LINK"
  gw=$(route -n get default 2>/dev/null | awk '/gateway:/{print $2; exit}')
  lan_if=$(route -n get "${gw:-1.1.1.1}" 2>/dev/null | awk '/interface:/{print $2; exit}')
  wifi_dev=$(networksetup -listallhardwareports 2>/dev/null \
    | awk '/Hardware Port: Wi-Fi/{getline; print $2; exit}')
  echo "  interface   ${lan_if:-none}"
  [ -n "$lan_if" ] && {
    echo "  address     $(ifconfig "$lan_if" 2>/dev/null | awk '/inet /{print $2; exit}')"
    echo "  router      ${gw:-unknown}"
  }
  if [ "$(uname)" = Darwin ]; then
    # Deliberately networksetup, not system_profiler SPAirPortDataType: the
    # latter takes ~6s on this machine, and both return nothing useful without
    # Location Services permission for the terminal, so paying 6s to print
    # "hidden" would be absurd. Grant the terminal that permission and this
    # starts resolving.
    ssid=$(networksetup -getairportnetwork "${wifi_dev:-en0}" 2>/dev/null \
      | sed -n 's/^Current Wi-Fi Network: //p')
    if [ -n "$ssid" ]; then echo "  ssid        $ssid"
    else echo "  ssid        hidden (grant the terminal Location Services to see it)"; fi
  fi
  echo "  public ip   $(curl -4 -s --max-time 5 https://1.1.1.1/cdn-cgi/trace 2>/dev/null | sed -n 's/^ip=//p')"
}

section_wg() {
  echo "WIREGUARD (nord0)"
  if [ -e /var/run/wireguard/nord0.name ]; then
    echo "  state       up"
    # The interface's own address is visible without root; `wg show` is not.
    ifconfig 2>/dev/null | awk '/^utun/{i=substr($1,1,length($1)-1)} /inet 10\.5\./{print "  address     " $2 " (" i ")"}'
    # The user-owned copy of the config is readable; /etc/wireguard is root-only.
    conf="$HOME/.secrets/nordvpn/nord0.conf"
    [ -r "$conf" ] && {
      echo "  server      $(sed -n 's/^# Server: //p' "$conf")"
      echo "  endpoint    $(sed -n 's/^Endpoint *= *//p' "$conf")"
    }
  else
    echo "  state       DOWN"
    echo "  bring up    sudo PATH=\"/opt/nanobrew/prefix/bin:\$PATH\" wg-quick up nord0"
  fi
}

section_nb() {
  echo "NETBIRD"
  s=$(netbird status -d 2>/dev/null)
  if [ -z "$s" ]; then echo "  state       daemon not responding"; return; fi
  printf '%s\n' "$s" | sed -n \
    -e 's/^NetBird IP: /  address     /p' \
    -e 's/^FQDN: /  fqdn        /p' \
    -e 's/^Peers count: /  peers       /p' \
    -e 's/^ *Connection type: /  conn type   /p' \
    -e 's/^ *Last WireGuard handshake: /  handshake   /p' \
    | head -8
}

# tmux hands back the full range name, "user|net_vpn", so match on a substring
# rather than the bare name.
case "${1:-net_link}" in
  *net_vpn*) section_wg; hr; section_nb; hr; section_link ;;
  *)         section_link; hr; section_wg; hr; section_nb ;;
esac

echo
echo "any key to close"
read -r -n1 -s
