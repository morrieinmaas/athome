"""
infra/inventory.py — pyinfra host inventory.

Each module-level list whose name doesn't start with `_` becomes a host
group pyinfra can target with `--limit <group>`. Each tuple is:

    (ssh_target, host_data_dict)

Where ssh_target is whatever you'd `ssh <target>` to (a hostname, IP, or
an alias from ~/.ssh/config). host_data_dict overrides any defaults set
in group_data/all.py.

Run:
    pyinfra infra/inventory.py infra/deploy.py                # all hosts
    pyinfra infra/inventory.py --limit arch infra/deploy.py   # one group
    pyinfra infra/inventory.py --limit <hostname> infra/deploy.py  # one host

Group named `arch`, `debian`, etc. is just an organizational convention —
pyinfra doesn't reason about it. The distro check happens in deploy.py
via the LinuxName fact at runtime.
"""

# ── Arch hosts ──────────────────────────────────────────────────────────
arch = [
    # Add your hosts here. Replace `<hostname>` with whatever you
    # `ssh <name>` to (LAN hostname, ~/.ssh/config alias, or IP).
    # Example — uncomment and edit:
    # (
    #     "<hostname>",
    #     {
    #         # ssh_user is the account pyinfra uses to LOGIN. On a fresh box
    #         # you'll usually start as `root` and flip to the created user
    #         # once it exists. After first deploy: ssh_user=<your-user>.
    #         "ssh_user": "root",
    #         "hostname": "<hostname>",
    #     },
    # ),
]

# ── Debian / Ubuntu cloud VMs ───────────────────────────────────────────
# Many cloud providers default to a Debian/Ubuntu image with root SSH;
# deploy.py detects the distro and adapts. Add hosts here as you
# provision them.
debian = [
    # Example — uncomment and edit:
    # ("scaleway-vm-1.example.com", {
    #     "ssh_user": "root",
    #     "hostname": "vm-1",
    # }),
]
