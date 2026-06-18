# athome — task runner. `just` is an OPTIONAL convenience; every recipe just
# wraps a script you can also call directly. Don't have just yet? Install it
# standalone (no mise needed), then you can even bootstrap with it:
#
#   curl --proto '=https' --tlsv1.2 -sSf https://just.systems/install.sh \
#     | bash -s -- --to ~/.local/bin
#   just bootstrap
#
# Run `just` with no args to list recipes.

# List available recipes.
default:
    @just --list

# ── bootstrap (works from bare metal) ───────────────────────────────────────

# Provision this machine. Extra args pass through, e.g. `just bootstrap --config bootstrap.local.toml`.
bootstrap *args:
    ./scripts/bootstrap.sh {{args}}

# Provision as a work machine.
bootstrap-work *args:
    ./scripts/bootstrap.sh --machine work {{args}}

# ── day-to-day chezmoi ──────────────────────────────────────────────────────

# Pull + apply (the daily sync).
update:
    chezmoi update -v

# Render source → $HOME (no pull).
apply:
    chezmoi apply -v

# What differs right now.
status:
    chezmoi status

# Line-by-line diff of pending changes.
diff:
    chezmoi diff

# ── quality ─────────────────────────────────────────────────────────────────

# Lint shell scripts (same shellcheck config as CI / pre-commit).
lint:
    pre-commit run shellcheck --all-files

# Run the bats test suite.
test:
    bats test

# Build + run the Linux bootstrap end-to-end in a container (docker/podman).
e2e:
    ./test/e2e/run.sh

# ── teardown ────────────────────────────────────────────────────────────────

# Reset for a from-scratch retry. Dry-run by default — see `--help`.
# e.g. `just teardown --all --execute`
teardown *args:
    ./scripts/teardown.sh {{args}}
