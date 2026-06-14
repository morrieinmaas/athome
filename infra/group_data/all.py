"""
infra/group_data/all.py — defaults applied to every host in inventory.py.

Per-host data in inventory.py overrides anything here. No personal
identifiers are baked into this file — values come from env vars when
you invoke pyinfra, with generic placeholder fallbacks if the env vars
aren't set:

    ATHOME_PYINFRA_USER=<linux-user>            \\
    ATHOME_PYINFRA_GITHUB_USER=<gh-handle>      \\
        pyinfra infra/inventory.py infra/deploy.py

Or stick them in your shell profile / direnv so you don't repeat
yourself every invocation.
"""

import os

# Username created on every target. Defaults to "user" — Arch's generic
# default — if you forget to set the env var. Works but probably not
# what you want for a real machine.
username = os.environ.get("ATHOME_PYINFRA_USER", "user")

# GitHub user whose public SSH keys get pulled into authorized_keys via
# https://github.com/<user>.keys. Make sure each machine's
# <hostname>_ed25519 pubkey is uploaded to this account first —
# bootstrap.sh does this on every machine automatically.
github_user = os.environ.get("ATHOME_PYINFRA_GITHUB_USER", "your-github-user")
