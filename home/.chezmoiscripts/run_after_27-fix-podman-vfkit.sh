#!/usr/bin/env bash
# nanobrew re-signs podman ad hoc and drops vfkit's com.apple.security.virtualization
# entitlement, so the applehv VM crashes or its API drops with EOF under load.
# `nb upgrade podman` brings the problem back, so this is a plain run_after (every
# apply): the check is a single codesign call and a no-op when vfkit is entitled.
# The logic lives in dot_local/bin/podman-fix-vfkit, also usable by hand.
set -euo pipefail

[ "$(uname -s)" = Darwin ] || exit 0

fix="$HOME/.local/bin/podman-fix-vfkit"
[ -x "$fix" ] || exit 0

"$fix"
