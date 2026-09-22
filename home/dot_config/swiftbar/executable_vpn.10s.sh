#!/usr/bin/env bash
# SwiftBar plugin: WireGuard (Nord) + NetBird state in the menu bar.
# Refresh interval is encoded in the FILENAME: vpn.10s.sh = every 10 seconds.
#
# Why the odd probe for WireGuard: `wg show interfaces` returns both tunnels
# (NetBird's utun is a WireGuard interface too), so it cannot tell them apart.
# wg-quick writes /var/run/wireguard/<name>.name as root-only, but the directory
# itself is listable, so testing for that file's existence is a privilege-free
# up/down check for this specific tunnel.
#
# SwiftBar runs plugins with a minimal PATH, so everything is absolute or set here.
export PATH="/opt/nanobrew/prefix/bin:/opt/homebrew/bin:/usr/bin:/bin"

if [[ -e /var/run/wireguard/nord0.name ]]; then
  wg_icon="🔒"; wg_txt="WireGuard (nord0): up"
else
  wg_icon="🔓"; wg_txt="WireGuard (nord0): DOWN"
fi

nb_status="$(netbird status 2>/dev/null)"
if grep -q 'Management: Connected' <<<"$nb_status"; then
  nb_icon="🕸"
  peers="$(grep -oE 'Peers count: [0-9]+/[0-9]+' <<<"$nb_status" | sed 's/Peers count: //')"
  nb_txt="NetBird: connected (peers ${peers:-?})"
else
  nb_icon="·"; nb_txt="NetBird: DOWN"
fi

echo "${wg_icon}${nb_icon}"
echo "---"
echo "$wg_txt"
echo "$nb_txt"
echo "---"
echo "Refresh | refresh=true"
