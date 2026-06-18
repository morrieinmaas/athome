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
# (paru builds), openssh for keygen. `rust` is pre-installed so building paru
# resolves the `cargo` makedepend unambiguously — otherwise pacman prompts "2
# providers available for cargo: rust/rustup", which a headless (no-TTY) build
# can't answer and `[Y/n]` on closed stdin aborts. Bonus: skips a slow ~274 MB
# emulated rust download mid-bootstrap. Refresh the package DB first.
RUN pacman -Syu --noconfirm --needed git curl sudo base-devel openssh which rust \
    && pacman -Scc --noconfirm

# makepkg/paru refuse to run as root → a passwordless-sudo build user.
RUN useradd -m -G wheel ci \
    && echo 'ci ALL=(ALL) NOPASSWD: ALL' > /etc/sudoers.d/ci \
    && chmod 0440 /etc/sudoers.d/ci

USER ci

# Pre-build paru from source ONCE here, as a cached image layer BEFORE the repo
# COPY (so editing the repo doesn't invalidate it). The Rust compile is paid once
# at image-build, never on `podman run`: the bootstrap's run_once_before_01 sees
# `command -v paru` and skips its own build. Source — not paru-bin — so it links
# against THIS image's libalpm; paru-bin's prebuilt binary dies the moment pacman
# bumps the libalpm soname ("libalpm.so.15: cannot open shared object file").
RUN git clone --depth 1 https://aur.archlinux.org/paru.git /tmp/paru \
    && (cd /tmp/paru && makepkg -si --noconfirm) \
    && rm -rf /tmp/paru

WORKDIR /home/ci/athome
COPY --chown=ci:ci . /home/ci/athome

# Headless skip-guards on; GITHUB_TOKEN is injected at run time (CI secret or a
# local `gh auth token`), not baked into the image.
ENV ATHOME_CI=1

# Run the real bootstrap with the dummy answers file, then assert the result.
CMD ["bash", "-lc", "./scripts/bootstrap.sh --config test/e2e/bootstrap.toml --machine personal && bash test/e2e/verify.sh"]
