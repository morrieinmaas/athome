#!/usr/bin/env bash
# tmux status-bar network segment: Wi-Fi (or wired) + VPN tunnels, as two pills
# in the same style as battery.sh / pet.sh.
#
# Pill 1, link:  Wi-Fi glyph + SSID, or an ethernet glyph when the default route
#                is on a wired interface. Red when there is no link at all.
# Pill 2, VPN:   padlock + which tunnels are up. "wg" is the wg-quick NordVPN
#                tunnel, "nb" is NetBird. Green when the WireGuard tunnel is up,
#                amber when only the mesh is, red when neither.
#
# Icons render from codepoints (no literal multibyte bytes in tracked files),
# matching battery.sh. These are Nerd Font glyphs, so they need the same
# JetBrains Mono Nerd Font the rest of the bar assumes.

# Nerd Font glyph from a codepoint.
glyph() { printf -v h '%08X' "0x$1"; printf "\\U$h"; }

opt() { tmux show -gv "$1" 2>/dev/null; }
fg=$(opt @theme_fg);       fg=${fg:-#3c3836}
bg=$(opt @theme_bg);       bg=${bg:-#fbf1c7}
blue=$(opt @theme_blue);   blue=${blue:-#458588}
green=$(opt @theme_green); green=${green:-#98971a}
red=$(opt @theme_red);     red=${red:-#cc241d}
yellow=$(opt @theme_yellow); yellow=${yellow:-#d79921}
pl=$(opt @pill_l); pr=$(opt @pill_r)

pill() { # $1=colour $2=text-colour $3=text
  printf '#[fg=%s,bg=default]%s#[fg=%s,bg=%s,bold] %s #[fg=%s,bg=default,nobold]%s' \
    "$1" "$pl" "$2" "$1" "$3" "$1" "$pr"
}

wifi_g=$(glyph F1EB)   # nf-fa-wifi
eth_g=$(glyph F0E8)    # nf-fa-sitemap
lock_g=$(glyph F023)   # nf-fa-lock
open_g=$(glyph F09C)   # nf-fa-unlock

# ── link ────────────────────────────────────────────────────────────────────
# Which interface actually carries the LAN, rather than whatever is merely up.
# Deliberately asks the routing table for the gateway's interface: on a machine
# with several tunnels the default route is a tunnel, so the LAN route is the
# honest way to find the physical link.
lan_if=""
if [ "$(uname)" = Darwin ]; then
  gw=$(route -n get default 2>/dev/null | awk '/gateway:/{print $2; exit}')
  [ -n "$gw" ] && lan_if=$(route -n get "$gw" 2>/dev/null | awk '/interface:/{print $2; exit}')
  wifi_dev=$(networksetup -listallhardwareports 2>/dev/null \
    | awk '/Hardware Port: Wi-Fi/{getline; print $2; exit}')
  if [ -n "$wifi_dev" ] && [ "$lan_if" = "$wifi_dev" ]; then
    ssid=$(networksetup -getairportnetwork "$wifi_dev" 2>/dev/null | sed -n 's/^Current Wi-Fi Network: //p')
    link=$(pill "$blue" "$bg" "$wifi_g ${ssid:-wifi}")
  elif [ -n "$lan_if" ]; then
    link=$(pill "$blue" "$bg" "$eth_g $lan_if")
  else
    link=$(pill "$red" "$bg" "$wifi_g offline")
  fi
else
  lan_if=$(ip route show default 2>/dev/null | awk '/default/{print $5; exit}')
  if [ -n "$lan_if" ]; then
    ssid=$(iwgetid -r 2>/dev/null)
    if [ -n "$ssid" ]; then link=$(pill "$blue" "$bg" "$wifi_g $ssid")
    else link=$(pill "$blue" "$bg" "$eth_g $lan_if"); fi
  else
    link=$(pill "$red" "$bg" "$wifi_g offline")
  fi
fi

# ── tunnels ─────────────────────────────────────────────────────────────────
# wg-quick writes /var/run/wireguard/<name>.name as root-only, but the directory
# lists fine, so existence is a privilege-free up/down probe. `wg show
# interfaces` is no good: NetBird's utun is a WireGuard interface too, so it
# cannot tell the two apart.
wg_up=0; nb_up=0
for n in /var/run/wireguard/*.name; do
  [ -e "$n" ] || continue
  case "${n##*/}" in nord*) wg_up=1 ;; esac
done

# ponytail: detects NetBird by its 100.64/10 overlay address rather than calling
# `netbird status`, which spawns a process on every status refresh. Costs us the
# ability to distinguish "daemon up but no peers"; swap in the real status call
# if that distinction ever matters.
if ifconfig 2>/dev/null | grep -qE 'inet 100\.(6[4-9]|[7-9][0-9]|1[01][0-9]|12[0-7])\.'; then
  nb_up=1
fi

if [ "$wg_up" = 1 ] && [ "$nb_up" = 1 ]; then vpn=$(pill "$green" "$bg" "$lock_g wg nb")
elif [ "$wg_up" = 1 ];                  then vpn=$(pill "$green" "$bg" "$lock_g wg")
elif [ "$nb_up" = 1 ];                  then vpn=$(pill "$yellow" "$bg" "$lock_g nb")
else                                         vpn=$(pill "$red" "$bg" "$open_g none"); fi

printf '%s %s' "$link" "$vpn"
