#!/usr/bin/env bash
# SwiftBar plugin: VPN egress + mesh membership in the menu bar.
# Refresh interval is encoded in the FILENAME: vpn.10s.sh = every 10 seconds.
#
# Provider-agnostic, same two probes the tmux pills use. An earlier version
# looked for wg-quick's /var/run/wireguard/nord0.name and shelled out to
# `netbird status`, which tied it to one specific arrangement that has since
# been abandoned. These work for any NetworkExtension VPN (NordVPN, Proton,
# Mullvad) and for any mesh (NordVPN Meshnet, Tailscale, NetBird).
#
# SwiftBar runs plugins with a minimal PATH, so everything is absolute.
export PATH="/usr/bin:/bin:/usr/sbin:/sbin"

# VPN egress: is the default route on a tunnel? That is the question that
# matters, and it needs no vendor CLI.
vpn_if=$(route -n get default 2>/dev/null | awk '/interface:/{print $2; exit}')
case "$vpn_if" in
  utun*|wg*|tun*|nordlynx) vpn="🔒" ;;
  *)                       vpn="🔓" ;;
esac

# Mesh: NordVPN Meshnet, Tailscale and NetBird all allocate from the RFC 6598
# shared range 100.64.0.0/10, so an address there means this machine is on one.
mesh_ip=$(ifconfig 2>/dev/null \
  | awk '/inet 100\.(6[4-9]|[7-9][0-9]|1[01][0-9]|12[0-7])\./{print $2; exit}')
[ -n "$mesh_ip" ] && mesh="🕸" || mesh="·"

echo "${vpn}${mesh}"
echo "---"
if [ "$vpn" = "🔒" ]; then echo "VPN: tunnelled via $vpn_if"; else echo "VPN: direct (no tunnel)"; fi
if [ -n "$mesh_ip" ]; then echo "Mesh: $mesh_ip"; else echo "Mesh: not connected"; fi
echo "---"
echo "Refresh | refresh=true"
