# test/e2e/Dockerfile — a clean Arch box to run the athome bootstrap end-to-end.
#
# Validates the Linux setup unattended: no gh login, no Bitwarden, dummy answers
# from test/e2e/bootstrap.toml, and ATHOME_CI=1 so the systemd/GUI/hardware
# scripts (niri, netbird, podman, …) skip cleanly. mise gets GITHUB_TOKEN from
# the build/run env to avoid the anon GitHub rate limit.
FROM archlinux:latest

# Disable pacman's seccomp/landlock sandbox: its syscall filter fails under qemu
# emulation (`restricting syscalls via seccomp: 22`) when building this amd64
# image on an arm64 host. Harmless on native amd64 CI; this is a throwaway box.
# Set in [options] so it also covers the bootstrap's own pacman package install.
RUN sed -i '/^\[options\]/a DisableSandbox' /etc/pacman.conf

# Base tools the bootstrap assumes present: git/curl/sudo, base-devel for AUR
# builds, openssh for keygen. `rust` is pre-installed so any AUR package with a
# `cargo` makedepend resolves it unambiguously — otherwise pacman prompts "2
# providers available for cargo: rust/rustup", which a headless (no-TTY) build
# can't answer and `[Y/n]` on closed stdin aborts. Bonus: skips a slow ~274 MB
# emulated rust download mid-bootstrap. Refresh the package DB first.
RUN pacman -Syu --noconfirm --needed git curl sudo base-devel openssh which rust \
    && pacman -Scc --noconfirm

# Install yay as a BINARY from chaotic-aur — NO source compile (building an AUR
# helper under qemu takes ages). chaotic-aur rebuilds against current Arch, so
# unlike the raw yay-bin AUR package it tracks the libalpm soname and just works
# (yay links libalpm dynamically via go-alpm/cgo, so a prebuilt -bin breaks on
# every libalpm bump — exactly the breakage paru-bin had). The bootstrap then
# skips its own yay build (`command -v yay`). Cached layer → done once at image
# build, never per `podman run`.
RUN pacman-key --init \
    && pacman-key --recv-key 3056513887B78AEB --keyserver keyserver.ubuntu.com \
    && pacman-key --lsign-key 3056513887B78AEB \
    && pacman -U --noconfirm \
         'https://cdn-mirror.chaotic.cx/chaotic-aur/chaotic-keyring.pkg.tar.zst' \
         'https://cdn-mirror.chaotic.cx/chaotic-aur/chaotic-mirrorlist.pkg.tar.zst' \
    && printf '\n[chaotic-aur]\nInclude = /etc/pacman.d/chaotic-mirrorlist\n' >> /etc/pacman.conf \
    && pacman -Sy --noconfirm yay

# makepkg/yay refuse to run as root → a passwordless-sudo build user.
RUN useradd -m -G wheel ci \
    && echo 'ci ALL=(ALL) NOPASSWD: ALL' > /etc/sudoers.d/ci \
    && chmod 0440 /etc/sudoers.d/ci

USER ci
WORKDIR /home/ci/athome
COPY --chown=ci:ci . /home/ci/athome

# Headless skip-guards on; GITHUB_TOKEN is injected at run time (CI secret or a
# local `gh auth token`), not baked into the image.
#
# NOTE: ATHOME_SKIP_PACKAGES (skipping slow build-from-source AUR packages) is
# NOT set here — it's injected by test/e2e/run.sh ONLY when the image runs
# emulated (e.g. an arm64 Mac running this amd64 image). CI runs natively, so it
# still installs and validates every package.
ENV ATHOME_CI=1

# Full lifecycle lives in test/e2e/scenario.sh (shared with Dockerfile.fedora):
# bootstrap→apply→verify, then teardown→verify-teardown (user data safe), then
# re-bootstrap→re-verify (round-trip). Single source of truth for both distros.
CMD ["bash", "-lc", "bash test/e2e/scenario.sh"]
