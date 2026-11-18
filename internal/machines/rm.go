package machines

import (
	"context"
	"errors"
	"fmt"
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

// Remove revokes a machine: its gh login and break-glass key on the box, its
// GitHub keys, its grofe entry and pinned host key, then the inventory entry.
// Without force the first failing step stops the run before the inventory
// changes. With force each failing step is a note and the run exits 0.
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

	list := []rmStep{
		{"gh auth logout on the box", func(ctx context.Context) error {
			if err := CheckInclude(d.Home); err != nil {
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
		{"GitHub keys", func(ctx context.Context) error { return gh.deleteByTitle(ctx, name) }},
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
			var kept []Machine
			for _, x := range ms {
				if x.Name != name {
					kept = append(kept, x)
				}
			}
			if inInventory {
				if err := Save(d.Home, kept); err != nil {
					return err
				}
			}
			if err := RemovePending(d.Home, name); err != nil {
				return err
			}
			if err := d.refresh(); err != nil {
				return err
			}
			if inInventory {
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
