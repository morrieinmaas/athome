#!/usr/bin/env bash
# tmux status-bar network segment: link + VPN tunnels, as two pills in the same
# style as battery.sh / pet.sh. Click either pill for detail (net-popup.sh).
#
# Pill 1, link:  Wi-Fi/ethernet glyph + the LAN address. The address is the
#                genuinely useful bit: it is what the break-glass path connects
#                to, and it drifts (this machine moved .156 -> .104 in a day).
# Pill 2, VPN:   padlock + which tunnels are up, and NetBird's peer count.
#
# Cost matters here: the bar re-renders every status-interval. Measured on this
# machine: ifconfig 7ms, netbird status 142ms, system_profiler SPAirPortDataType
# 6020ms. So system_profiler is banned from this script and lives in the popup.
#
# SSID is deliberately NOT shown: recent macOS redacts it (system_profiler
# returns "<redacted>", networksetup claims "not associated") unless the calling
# app holds Location Services permission, which a terminal normally does not.
#
# Icons render from codepoints (no literal multibyte bytes in tracked files),
# matching battery.sh. Nerd Font glyphs, same font the rest of the bar assumes.

glyph() { printf -v h '%08X' "0x$1"; printf "\\U$h"; }

opt() { tmux show -gv "$1" 2>/dev/null; }
bg=$(opt @theme_bg);         bg=${bg:-#fbf1c7}
blue=$(opt @theme_blue);     blue=${blue:-#458588}
green=$(opt @theme_green);   green=${green:-#98971a}
red=$(opt @theme_red);       red=${red:-#cc241d}
yellow=$(opt @theme_yellow); yellow=${yellow:-#d79921}
pl=$(opt @pill_l); pr=$(opt @pill_r)

# The click target is NOT set here. tmux honours #[range=...] reliably only
# when it appears in the status option itself, not when it arrives via a #()
# command's output, so tmux.conf wraps this whole script in one
# #[range=user|net] instead. $2 is kept for readability at the call sites.
pill() { # $1=colour $2=label (unused, documents which pill) $3=text
  printf '#[fg=%s,bg=default]%s#[fg=%s,bg=%s,bold] %s #[fg=%s,bg=default,nobold]%s' \
    "$1" "$pl" "$bg" "$1" "$3" "$1" "$pr"
}

wifi_g=$(glyph F1EB)   # nf-fa-wifi
eth_g=$(glyph F0E8)    # nf-fa-sitemap
lock_g=$(glyph F023)   # nf-fa-lock
open_g=$(glyph F09C)   # nf-fa-unlock
peer_g=$(glyph F0C0)   # nf-fa-users, marks the NetBird peer tally

# ── link ────────────────────────────────────────────────────────────────────
# Ask the routing table for the GATEWAY's interface, not the default route:
# with a tunnel up the default route is the tunnel and tells you nothing about
# the physical link.
if [ "$(uname)" = Darwin ]; then
  gw=$(route -n get default 2>/dev/null | awk '/gateway:/{print $2; exit}')
  lan_if=$(route -n get "${gw:-1.1.1.1}" 2>/dev/null | awk '/interface:/{print $2; exit}')
  wifi_dev=$(networksetup -listallhardwareports 2>/dev/null \
    | awk '/Hardware Port: Wi-Fi/{getline; print $2; exit}')
else
  lan_if=$(ip route show default 2>/dev/null | awk '/default/{print $5; exit}')
  wifi_dev=""
  for w in /sys/class/net/*/wireless; do
    [ -d "$w" ] || continue
    w=${w%/wireless}; wifi_dev=${w##*/}; break
  done
fi

lan_ip=""
[ -n "$lan_if" ] && lan_ip=$(ifconfig "$lan_if" 2>/dev/null | awk '/inet /{print $2; exit}')

# Shows the ADDRESS, not the network name, on purpose. macOS treats the SSID as
# location data (network names are how device positioning works without GPS) and
# redacts it, along with the BSSID, from any caller lacking Location Services
# permission. Turning that on for a terminal is a real privacy trade for a
# cosmetic label, so it is declined here. The address is the more useful value
# anyway: it is what the break-glass path connects to, and it drifts.
if [ -z "$lan_if" ] || [ -z "$lan_ip" ]; then
  link=$(pill "$red" net_link "$wifi_g offline")
elif [ -n "$wifi_dev" ] && [ "$lan_if" = "$wifi_dev" ]; then
  link=$(pill "$blue" net_link "$wifi_g $lan_ip")
else
  link=$(pill "$blue" net_link "$eth_g $lan_if $lan_ip")
fi

# ── tunnels ─────────────────────────────────────────────────────────────────
# wg-quick writes /var/run/wireguard/<name>.name as root-only, but the directory
# lists fine, so existence is a privilege-free up/down probe. `wg show
# interfaces` is no good: NetBird's utun is a WireGuard interface too, so it
# cannot tell the two apart.
wg_up=0
for n in /var/run/wireguard/nord*.name; do [ -e "$n" ] && wg_up=1; done

peers=""
nb_raw=$(netbird status 2>/dev/null)
case "$nb_raw" in
  *"Management: Connected"*)
    peers=$(printf '%s\n' "$nb_raw" | sed -n 's/.*Peers count: \([0-9]*\/[0-9]*\).*/\1/p' | head -1)
    ;;
esac

# "$peers" is NetBird's connected/known peer count, e.g. 0/1 means one peer is
# registered on the mesh but currently offline. Labelled explicitly, because
# a bare "0/1" in a status bar reads as an error rather than a peer tally.
# TWO separate pills, not one. An earlier version rendered "wg <glyph> 0/1" in a
# single pill, which reads as though the 0/1 belongs to WireGuard. It does not:
# wg is the NordVPN tunnel, which is simply up or down with no count, while 0/1
# is NetBird's peer tally. Two unrelated facts, so two pills.
if [ "$wg_up" = 1 ]; then
  wg_pill=$(pill "$green" net_vpn "$lock_g wg")
else
  wg_pill=$(pill "$red" net_vpn "$open_g wg")
fi

# Colour tracks the DAEMON, not the peer count. NetBird being up with no peer
# online is normal: it means this machine is on the mesh and the other machine
# happens to be off. Coloring that amber implied a fault here when the fault, if
# any, is on the far end. Red is reserved for "this machine is not on the mesh".
case "$peers" in
  "") nb_pill=$(pill "$red" net_vpn "$peer_g nb off") ;;
  *)  nb_pill=$(pill "$green" net_vpn "$peer_g nb $peers") ;;
esac

printf '%s %s %s' "$link" "$wg_pill" "$nb_pill"
