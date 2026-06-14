# NetBird (mesh VPN)

NetBird replaces the previous Tailscale + Headscale setup. It's a WireGuard-based
overlay mesh with SSO sign-in and a coordination/management server.

**Why NetBird over Tailscale+Headscale:** Tailscale's control plane is proprietary,
and Headscale is a community catch-up reimplementation of it — a second-class path
for a FOSS-first setup. NetBird is fully open source end-to-end: the same server you
run yourself is the one the Cloud runs. So "SaaS now, self-host later" is a
**no-feature-loss** exit, not a downgrade.

---

## Default: NetBird Cloud (free tier)

The free tier covers up to **5 users / 100 machines** — vastly over-provisioned for a
solo setup. This is the default; no `netbird_management_url` needed.

1. **Sign up** at <https://app.netbird.io> (GitHub/Google/email SSO).
2. **Install the client** — handled by athome:
   - Linux (Arch): `netbird` from the AUR via `run_onchange_02`; the daemon is enabled
     by `run_once_08`.
   - macOS: installed by `run_once_08` via `nb install netbirdio/tap/netbird`
     (NetBird isn't in Homebrew core; nanobrew supports third-party taps, with a
     real-brew fallback if `nb` is absent).
3. **Join the mesh:**
   ```bash
   sudo netbird up            # opens a browser for SSO, then joins your network
   netbird status             # peers, IPs, connection state
   ```

### Headless / unattended machines

Use a **setup key** from the dashboard (Setup Keys → create) instead of interactive SSO:

```bash
sudo netbird up --setup-key <KEY>
```

Setup keys can be reusable or one-off, and can be ephemeral (peer auto-removed when
offline) — ideal for servers and CI.

---

## The self-host exit (no penalty, future)

When/if you want sole-tenancy, run your own NetBird management server and point clients
at it — **same binary, same features**, just a different management URL:

```bash
sudo netbird up --management-url https://netbird.<your-domain> --setup-key <KEY>
```

Set the athome prompt `netbirdManagementUrl` to `https://netbird.<your-domain>` so the
sign-in command printed by `run_once_08` is always correct.

NetBird ships an official self-hosting quickstart (Docker Compose: management server,
signal server, dashboard, and a Zitadel IdP) — see
<https://docs.netbird.io/selfhosted/selfhosted-quickstart>. It colocates fine on a small
Scaleway DEV1-S (`nl-ams-1`, 2 vCPU / 2 GB) behind Caddy, alongside other self-hosted
services.

---

## Rollback (to Tailscale + Headscale)

If NetBird ever needs to be backed out before it's fully trusted:

```bash
# macOS
brew install --cask tailscale
# Arch
sudo pacman -S --needed tailscale && sudo systemctl enable --now tailscaled
sudo tailscale up --login-server=https://<headscale-host>
```

(The Headscale deploy guide was retired in the tooling migration; recover it from git
history at `docs/headscale-self-host.md` if needed.)
