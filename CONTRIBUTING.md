# Contributing

## Fork first, honestly

This is one person's workstation configuration. It is published because the wiring is useful
to read and steal from, not because it wants to become a general-purpose framework.

So the expected mode is **fork it and make it yours**. Changing the git identities, the package
list and the theme is not a contribution, it is the intended use. You do not need permission
and you do not need to upstream it.

Pull requests are welcome for things that are genuinely general: a bug that would bite any
user, a portability fix, a packaging correction, a doc that is wrong or stale. Changes that
only make sense for one person's setup belong in that person's fork.

Open an issue before a large change. It may already be a deliberate choice, in which case
[AGENTS.md](AGENTS.md) probably explains why.

## Read AGENTS.md before changing anything

[AGENTS.md](AGENTS.md) is the operational runbook: the non-obvious rules about capturing user
state before `chezmoi apply --force`, which paths chezmoi manages, tag discipline, and the
failure modes already hit and fixed. Most surprising-looking code is explained there.

## Where a change belongs

The one rule that trips people up, from AGENTS.md:

| What you are adding | Where it goes |
| --- | --- |
| A portable single-binary CLI | `home/dot_config/mise/config.toml` under `[tools]` |
| A GUI app, font, library, system service, or anything needing root or capabilities | `home/.chezmoidata/packages.yaml` |
| Anything invoked **outside an interactive shell** (git hooks, systemd units, the bootstrap) | `packages.yaml`, never mise |

That last row is the subtle one. mise is only activated in interactive shells, so a mise shim
is not on `PATH` for a git hook or a service, and the tool silently does nothing. This is why
`gitleaks` and `rbw` are native packages rather than mise tools.

Linux-only config is gated through the existing block in `home/.chezmoiignore`, not through
per-file conditionals.

## Testing

The end-to-end suite bootstraps a real machine inside a container, which is the only way to
catch the ordering bugs that matter:

```bash
test/e2e/run.sh            # Arch and Fedora
```

CI runs the same thing on every push, plus `shellcheck`, `shfmt` and `gitleaks`. Run them
locally before opening a PR:

```bash
mise run lint
```

Note that `*.tmpl` and `*.zsh` files are excluded from shellcheck and shfmt on purpose: they
are Go templates and zsh respectively, and neither tool parses them. Template behaviour is
covered by the e2e instead. To lint a rendered template, render it first:

```bash
chezmoi execute-template < home/.chezmoiscripts/run_once_11-setup-niri-noctalia.sh.tmpl | shellcheck -
```

## Commits

- Conventional commits: `type(scope): description`.
- Explain **why** in the body, not what. The diff already says what.
- No `Co-Authored-By` lines and no AI attribution.
- Do not commit anything under `~/.secrets`, and do not add identifying values (emails,
  hostnames, employer or client names) to tracked files. Identity comes from chezmoi prompts
  so this repo stays publishable; keep it that way.

## Tags

`scripts/bootstrap.sh` defaults `--ref` to the newest git tag, so a stale tag means every
fresh install gets old behaviour. If your change should be the default for new installs, say
so in the PR and it will be tagged on merge. See the tag discipline section in AGENTS.md.
