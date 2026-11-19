package machines

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/morrieinmaas/athome/internal/config"
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

// remover is one machine's removal. m is the inventory entry, else the pending
// one, else just the name (force only); known says whether a box can be reached.
type remover struct {
	d           Deps
	name        string
	m           *Machine
	known       bool
	inInventory bool
	r           Remote
	gh          *ghClient
}

// Remove revokes a machine: its GitHub keys first, then its gh login and
// break-glass key on the box, then its grofe entry, pinned host key and
// inventory entry. Without force the first failing step stops the run before
// the inventory changes. With force each failing step is a note and the run
// exits 0.
func Remove(ctx context.Context, d Deps, name string, force bool) error {
	rm, err := newRemover(d, name, force)
	if err != nil {
		return err
	}
	list := []rmStep{
		{"GitHub keys", rm.deleteGitHubKeys},
		{"gh auth logout on the box", rm.ghLogout},
		{"break-glass key on the box", rm.dropBreakGlass},
		{"grofe machine rm", rm.grofeRm},
		{"pinned host key", rm.unpinHostKey},
		{"inventory", rm.dropInventory},
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

// newRemover finds what is known about name: its inventory entry, else its
// pending add, else (force only) just the name.
func newRemover(d Deps, name string, force bool) (*remover, error) {
	ms, err := Load(d.Home)
	if err != nil {
		return nil, err
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
			var bad *InvalidPendingError
			if !errors.As(err, &bad) || !force {
				return nil, err
			}
			d.say("note: %v; removing by name only", err)
			m = nil
		}
	}
	known := m != nil
	if m == nil {
		if !force {
			return nil, fmt.Errorf("machine %s not found", name)
		}
		if !config.MachineNameRe.MatchString(name) {
			return nil, fmt.Errorf("machine %s: name: must match %s", name, config.MachineNameRe)
		}
		m = &Machine{Name: name}
	}
	return &remover{d: d, name: name, m: m, known: known, inInventory: inInventory, r: d.remote(), gh: d.gh(*m)}, nil
}

// boxReady gates both box steps: with no entry to say where the box is, or
// without the generated ssh config in play, no ssh call is made at all. The
// config is rewritten first, so the machine's pinned Host block is in it.
func (rm *remover) boxReady() error {
	if !rm.known {
		return errors.New("no inventory entry or pending add, so no box is reached")
	}
	return rm.d.boxConfig()
}

func (rm *remover) deleteGitHubKeys(ctx context.Context) error {
	n, err := rm.gh.deleteByTitle(ctx, rm.name)
	if err != nil {
		return err
	}
	if n == 0 {
		acct := rm.m.GitHubAccount
		if acct == "" {
			acct, _ = rm.gh.login(ctx)
		}
		if acct == "" {
			acct = "the account gh is logged in to"
		}
		rm.d.say("note: no GitHub key titled athome-%s or athome-%s-sign in account %s", rm.name, rm.name, acct)
	}
	return nil
}

func (rm *remover) ghLogout(ctx context.Context) error {
	if err := rm.boxReady(); err != nil {
		return err
	}
	res, err := rm.r.Exec(ctx, rm.name, ghLogoutScript, "")
	if err == nil && res.Code != 0 && strings.Contains(res.Stderr, "not logged") {
		return nil
	}
	return okOrErr("gh auth logout", rm.name, res, err)
}

func (rm *remover) dropBreakGlass(ctx context.Context) error {
	if rm.m.Scope == "" {
		rm.d.say("note: no scope known for %s, so no break-glass key is removed", rm.name)
		return nil
	}
	if err := rm.boxReady(); err != nil {
		return err
	}
	pub, err := keys.BreakGlassPub(ctx, rm.d.Run, rm.m.Scope)
	if errors.Is(err, keys.ErrNoItem) {
		return nil
	}
	if err != nil {
		return err
	}
	res, err := rm.r.Exec(ctx, rm.name, dropKeyScript, "", keys.PubBlob(pub))
	return okOrErr("remove the break-glass key", rm.name, res, err)
}

func (rm *remover) grofeRm(ctx context.Context) error {
	has, ok, err := rm.d.grofeHas(ctx, rm.name)
	if err != nil || !ok || !has {
		return err
	}
	g, _ := rm.d.grofe()
	res, err := rm.d.Run.Run(ctx, run.Cmd{Name: g, Args: []string{"machine", "rm", rm.name}})
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("grofe machine rm %s failed: %s", rm.name, strings.TrimSpace(res.Stderr))
	}
	return nil
}

func (rm *remover) unpinHostKey(ctx context.Context) error {
	if !fileExists(KnownHosts(rm.d.Home)) {
		return nil
	}
	if ok, err := rm.d.hostKeyPinned(ctx, rm.name); err != nil || !ok {
		return err
	}
	res, err := rm.d.Run.Run(ctx, run.Cmd{Name: "ssh-keygen", Args: []string{"-R", rm.name, "-f", KnownHosts(rm.d.Home)}})
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("ssh-keygen -R %s failed: %s", rm.name, strings.TrimSpace(res.Stderr))
	}
	return nil
}

func (rm *remover) dropInventory(ctx context.Context) error {
	changed := false
	if rm.inInventory {
		var err error
		changed, err = rm.d.updateInventory(func(ms []Machine) ([]Machine, bool, error) {
			kept := slices.DeleteFunc(slices.Clone(ms), func(x Machine) bool { return x.Name == rm.name })
			return kept, len(kept) != len(ms), nil
		})
		if err != nil {
			return err
		}
	}
	if err := RemovePending(rm.d.Home, rm.name); err != nil {
		return err
	}
	if err := rm.d.refresh(); err != nil {
		return err
	}
	if changed {
		rm.d.backup(ctx)
	}
	return nil
}
