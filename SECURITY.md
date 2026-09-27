# Security

## Reporting a vulnerability

Please report suspected vulnerabilities through
[GitHub's private vulnerability reporting](https://github.com/morrieinmaas/athome/security/advisories/new)
rather than a public issue, so a fix can land before the details are public.

This is a personal dotfiles repository maintained in spare time. There is no SLA. Expect a
best-effort response, and assume nothing about timelines.

## What this repository actually does to a machine

Read this before running `scripts/bootstrap.sh`. It is not a library you import; it is a
script that configures a whole workstation, and it does several things that deserve informed
consent.

- **It runs with `sudo`.** Package installation, display-manager setup and a few system files
  need root. Read `scripts/bootstrap.sh` and `home/.chezmoiscripts/` before running them, the
  same way you would with any provisioning script.
- **It generates SSH keys and uploads the public halves to your GitHub account.** It needs
  `gh` authenticated as you.
- **It installs software from third-party sources**: Homebrew/nanobrew formulae and casks, the
  Arch User Repository, Fedora COPRs, the mise tool registry, and Hugging Face for model
  weights if you enable the optional pieces. Each is a supply-chain trust decision you are
  making, not one this repo can make for you.

## Deliberate trade-offs you inherit by forking

These are choices, not oversights. They are reasonable for a single-user laptop and may not be
reasonable for you.

- **SSH keys are generated without a passphrase** (`ssh-keygen -N ""`). They are pinned
  per-host with `IdentityFile` + `IdentitiesOnly`, so no agent is needed and commit signing
  works unattended. The cost is that anyone who can read the key file can use it. If your
  threat model includes local attackers or shared machines, add a passphrase and reintroduce
  an agent.
- **The package prefix is chown'd to your user** (`/opt/nanobrew` on macOS). This is how
  Homebrew-style prefixes normally work, and it means anything running as root out of that
  prefix executes user-writable code. The optional NordVPN `nord0` daemon does exactly that,
  and its own comments state the trade-off; its launchd entry point is kept root-owned in
  `/usr/local/libexec` to narrow the surface.
- **Secrets have two models**, documented in [docs/secrets.md](docs/secrets.md). The default
  keeps a plaintext `~/.secrets/<repo>/.env` at mode 600 so it works offline; the alternative
  pulls from Bitwarden at runtime and writes nothing to disk. Pick deliberately.
- **`~/.secrets/` is never chezmoi-managed.** Nothing in this repo will commit it. That
  separation is the reason the dotfiles can manage `~/.config/*` freely.

## What is checked automatically

- **gitleaks** runs in CI and as a global pre-commit hook, with the default rule set plus
  extra rules for age, OpenPGP and OpenSSH private-key material. The allowlist is limited to
  literal placeholder strings.
- **shellcheck** and **shfmt** run in CI over every tracked shell script.
- An **end-to-end bootstrap** runs in Arch and Fedora containers on every push.

None of that proves the repo is safe to run on your machine. It proves it is internally
consistent. Read the scripts.
