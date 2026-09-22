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

# tmux runs status commands in the SERVER's environment, which never sources
# ~/.zshenv. Without this, `netbird` is not found and the pill reports "nb off"
# while NetBird is connected, which is worse than no pill at all. It only looks
# fine on a server that happened to inherit a login shell's PATH; one started by
# launchd at boot will not have.
export PATH="/opt/nanobrew/prefix/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"

# printf's \U escape needs bash 4+. macOS ships bash 3.2 as /bin/bash, and with
# a lean PATH `/usr/bin/env bash` resolves to exactly that, which renders the
# literal text \U0000F1EB into the status bar. The shebang is resolved before
# any PATH we set above, so re-exec under a modern bash when we are on an old
# one. If none is found we fall through and the glyphs degrade, which is no
# worse than not doing this.
if [ "${BASH_VERSINFO[0]:-0}" -lt 4 ]; then
  for _b in /opt/nanobrew/prefix/bin/bash /opt/homebrew/bin/bash /usr/local/bin/bash; do
    [ -x "$_b" ] && exec "$_b" "$0" "$@"
  done
fi

glyph() { printf -v h '%08X' "0x$1"; printf "\\U$h"; }

# ONE tmux round-trip, not one per option. `tmux show -gv` costs ~13ms and this
# script reads eight options, so the naive version spent ~100ms of its budget
# just asking tmux the same question eight times. Parsed in-process instead,
# which needs the bash 4+ we already re-exec into above.
declare -A _TOPT
while IFS=' ' read -r _k _v; do
  _v=${_v%\"}; _v=${_v#\"}
  [ -n "$_k" ] && _TOPT[$_k]=$_v
done <<< "$(tmux show -g 2>/dev/null)"
opt() { printf '%s' "${_TOPT[$1]-}"; }
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
# Do NOT derive the physical link from the routing table. An earlier version
# asked for the default route's gateway interface; that survives wg-quick only
# because wg-quick installs 0.0.0.0/1 + 128.0.0.0/1 and leaves 0.0.0.0/0 alone.
# A VPN that installs a REAL default route (NordVPN's app does; so does any
# exit-node setup) makes that lookup return the tunnel, and the bar then shows
# an ethernet glyph and a meaningless VPN-internal address while you are on
# Wi-Fi. Instead walk the network services in macOS's own priority order and
# take the first physical en* that actually holds an address, which is the
# primary link regardless of which tunnel owns the default route.
if [ "$(uname)" = Darwin ]; then
  # The two networksetup calls below cost ~160ms together, which is a lot for
  # something the bar runs every status-interval. The answers (which en* is
  # primary, which is the Wi-Fi device) change only when you plug or unplug
  # something, so cache them. Self-healing: the cached interface is only trusted
  # while it still holds an address, so unplugging invalidates it immediately
  # rather than waiting for the TTL.
  _cache="${TMPDIR:-/tmp}/.tmux-net-iface.$UID"
  lan_if=""; wifi_dev=""
  if [ -r "$_cache" ]; then
    # shellcheck source=/dev/null
    . "$_cache"
    ipconfig getifaddr "${lan_if:-none}" >/dev/null 2>&1 || { lan_if=""; wifi_dev=""; }
  fi
  if [ -z "$lan_if" ]; then
    while read -r _dev; do
      case "$_dev" in en*) ;; *) continue ;; esac
      if ipconfig getifaddr "$_dev" >/dev/null 2>&1; then lan_if="$_dev"; break; fi
    done < <(networksetup -listnetworkserviceorder 2>/dev/null \
               | sed -n 's/.*Device: \([^)]*\)).*/\1/p')
    wifi_dev=$(networksetup -listallhardwareports 2>/dev/null \
      | awk '/Hardware Port: Wi-Fi/{getline; print $2; exit}')
    printf 'lan_if=%s\nwifi_dev=%s\n' "$lan_if" "$wifi_dev" > "$_cache" 2>/dev/null || true
  fi
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
