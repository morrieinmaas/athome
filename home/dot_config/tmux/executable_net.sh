#!/usr/bin/env bash
# tmux status-bar network segment: link + VPN tunnels, as two pills in the same
# style as battery.sh / pet.sh. Click either pill for detail (net-popup.sh).
#
# Pill 1, link:  Wi-Fi/ethernet glyph + the LAN address. The address is the
#                genuinely useful bit: it is what you connect to, and it drifts
#                (this machine moved .156 -> .104 within a day).
# Pill 2, VPN:   is the default route on a tunnel, i.e. is traffic leaving
#                through a VPN. Provider-agnostic.
# Pill 3, mesh:  this machine's 100.64.0.0/10 address, if it has one. Works for
#                NordVPN Meshnet, Tailscale and NetBird alike, since all three
#                allocate from that range.
#
# Cost matters here: the bar re-renders every status-interval. Measured on this
# machine: ifconfig 7ms, networksetup ~160ms (cached below), system_profiler
# SPAirPortDataType 6020ms. system_profiler is banned from this script entirely;
# the popup does the expensive work.
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

# Arch (and Fedora minimal) don't ship net-tools, so there is no `ifconfig` and
# the address lookups below came back empty: the bar said "offline" on a live
# link. Emulate just the two line shapes the awk parsers read, "<if>:" headers
# and "inet <addr>" rows, from iproute2.
if ! command -v ifconfig >/dev/null 2>&1 && command -v ip >/dev/null 2>&1; then
  ifconfig() {
    ip -4 -o addr show ${1:+dev "$1"} 2>/dev/null \
      | awk '{sub(/\/.*/, "", $4); print $2 ":"; print "\tinet " $4}'
  }
fi

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
muted=$(opt @theme_muted);   muted=${muted:-#7c6f64}
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
peer_g=$(glyph F0C0)   # nf-fa-users, marks the mesh address

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
# Provider-agnostic on purpose. This used to probe wg-quick's
# /var/run/wireguard/nord*.name and call `netbird status`, which tied the bar to
# one specific arrangement that has since been abandoned. Both probes below work
# for any NetworkExtension VPN (NordVPN, Proton, Mullvad) and for a hand-rolled
# wg-quick tunnel alike.

# VPN egress: is the DEFAULT ROUTE on a tunnel? That is the question that
# actually matters ("is my traffic leaving through a VPN"), and it needs no
# vendor CLI. Note this is the default route deliberately, unlike the link
# detection above, which needs the physical interface instead.
vpn_if=""
if [ "$(uname)" = Darwin ]; then
  vpn_if=$(route -n get default 2>/dev/null | awk '/interface:/{print $2; exit}')
else
  vpn_if=$(ip route show default 2>/dev/null | awk '/default/{print $5; exit}')
fi
case "$vpn_if" in utun*|wg*|tun*|nordlynx) vpn_up=1 ;; *) vpn_up=0 ;; esac

# Mesh: NordVPN Meshnet (and Tailscale, and NetBird) all allocate from the
# RFC 6598 shared range 100.64.0.0/10, so an address in that range on any
# interface means "this machine is on a mesh". Cheap: one ifconfig, ~7ms.
mesh_ip=$(ifconfig 2>/dev/null \
  | awk '/inet 100\.(6[4-9]|[7-9][0-9]|1[01][0-9]|12[0-7])\./{print $2; exit}')

# Muted, not red, when either is off. Both are things you switch off on purpose
# all the time now, so red would cry wolf; red is reserved for a broken link.
if [ "$vpn_up" = 1 ]; then
  vpn_pill=$(pill "$green" net_vpn "$lock_g vpn")
else
  vpn_pill=$(pill "$muted" net_vpn "$open_g direct")
fi

if [ -n "$mesh_ip" ]; then
  mesh_pill=$(pill "$green" net_vpn "$peer_g $mesh_ip")
else
  mesh_pill=$(pill "$muted" net_vpn "$peer_g no mesh")
fi

printf '%s %s %s' "$link" "$vpn_pill" "$mesh_pill"
