package machines

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/morrieinmaas/athome/internal/keys"
	"github.com/morrieinmaas/athome/internal/run"
)

const (
	ghLogoutScript = `gh auth logout --hostname github.com`
	dropKeyScript  = `f="$HOME/.ssh/authorized_keys"
[ -f "$f" ] || exit 0
n="$f.athome"
umask 077
awk -v b="$1" '{ for (i = 1; i <= NF; i++) if ($i == b) next } 1' "$f" > "$n" && chmod 600 "$n" && mv "$n" "$f"`
)

type rmStep struct {
	name string
	run  func(ctx context.Context) error
}

// Remove revokes a machine: its GitHub keys first, then its gh login and
// break-glass key on the box, then its grofe entry, pinned host key and
// inventory entry. Without force the first failing step stops the run before
// the inventory changes. With force each failing step is a note and the run
// exits 0.
func Remove(ctx context.Context, d Deps, name string, force bool) error {
	ms, err := Load(d.Home)
	if err != nil {
		return err
	}
	var m *Machine
	inInventory := hasMachine(ms, name)
	for i := range ms {
		if ms[i].Name == name {
			m = &ms[i]
		}
	}
	if m == nil {
		if m, err = LoadPending(d.Home, name); err != nil {
			return err
		}
	}
	known := m != nil
	if m == nil {
		if !force {
			return fmt.Errorf("machine %s not found", name)
		}
		if !nameRe.MatchString(name) {
			return fmt.Errorf("machine %s: name: must match %s", name, nameRe)
		}
		m = &Machine{Name: name}
	}
	r := d.remote()
	gh := d.gh(*m)

	// boxReady gates both box steps: with no entry to say where the box is, or
	// without the generated ssh config in play, no ssh call is made at all.
	boxReady := func() error {
		if !known {
			return errors.New("no inventory entry or pending add, so no box is reached")
		}
		return CheckInclude(d.Home)
	}

	list := []rmStep{
		{"GitHub keys", func(ctx context.Context) error {
			n, err := gh.deleteByTitle(ctx, name)
			if err != nil {
				return err
			}
			if n == 0 {
				acct := m.GitHubAccount
				if acct == "" {
					acct, _ = gh.login(ctx)
				}
				if acct == "" {
					acct = "the account gh is logged in to"
				}
				d.say("note: no GitHub key titled athome-%s or athome-%s-sign in account %s", name, name, acct)
			}
			return nil
		}},
		{"gh auth logout on the box", func(ctx context.Context) error {
			if err := boxReady(); err != nil {
				return err
			}
			res, err := r.Exec(ctx, name, ghLogoutScript, "")
			if err == nil && res.Code != 0 && strings.Contains(res.Stderr, "not logged") {
				return nil
			}
			return okOrErr("gh auth logout", name, res, err)
		}},
		{"break-glass key on the box", func(ctx context.Context) error {
			if m.Scope == "" {
				d.say("note: no scope known for %s, so no break-glass key is removed", name)
				return nil
			}
			if err := boxReady(); err != nil {
				return err
			}
			pub, err := keys.BreakGlassPub(ctx, d.Run, m.Scope)
			if errors.Is(err, keys.ErrNoItem) {
				return nil
			}
			if err != nil {
				return err
			}
			res, err := r.Exec(ctx, name, dropKeyScript, "", blobOf(pub))
			return okOrErr("remove the break-glass key", name, res, err)
		}},
		{"grofe machine rm", func(ctx context.Context) error {
			has, ok, err := d.grofeHas(ctx, name)
			if err != nil || !ok || !has {
				return err
			}
			g, _ := d.grofe()
			res, err := d.Run.Run(ctx, run.Cmd{Name: g, Args: []string{"machine", "rm", name}})
			if err != nil {
				return err
			}
			if res.Code != 0 {
				return fmt.Errorf("grofe machine rm %s failed: %s", name, strings.TrimSpace(res.Stderr))
			}
			return nil
		}},
		{"pinned host key", func(ctx context.Context) error {
			if !fileExists(KnownHosts(d.Home)) {
				return nil
			}
			if ok, err := d.hostKeyPinned(ctx, name); err != nil || !ok {
				return err
			}
			res, err := d.Run.Run(ctx, run.Cmd{Name: "ssh-keygen", Args: []string{"-R", name, "-f", KnownHosts(d.Home)}})
			if err != nil {
				return err
			}
			if res.Code != 0 {
				return fmt.Errorf("ssh-keygen -R %s failed: %s", name, strings.TrimSpace(res.Stderr))
			}
			return nil
		}},
		{"inventory", func(ctx context.Context) error {
			changed := false
			if inInventory {
				var err error
				changed, err = d.updateInventory(func(ms []Machine) ([]Machine, bool, error) {
					kept := slices.DeleteFunc(slices.Clone(ms), func(x Machine) bool { return x.Name == name })
					return kept, len(kept) != len(ms), nil
				})
				if err != nil {
					return err
				}
			}
			if err := RemovePending(d.Home, name); err != nil {
				return err
			}
			if err := d.refresh(); err != nil {
				return err
			}
			if changed {
				d.backup(ctx)
			}
			return nil
		}},
	}
	for _, s := range list {
		err := s.run(ctx)
		if err == nil {
			continue
		}
		if !force {
			return fmt.Errorf("athome machine rm: %s: %w", s.name, err)
		}
		d.say("note: %s: %v", s.name, err)
	}
	d.say("✓ machine %s removed", name)
	return nil
}
