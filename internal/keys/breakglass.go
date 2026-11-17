package keys

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/morrieinmaas/athome/internal/run"
)

// ErrNoItem means the Bitwarden item does not exist.
var ErrNoItem = errors.New("no such Bitwarden item")

// BreakGlassItem is the Bitwarden item holding a scope's break-glass key.
func BreakGlassItem(scope string) string { return "athome/" + scope + "/break-glass" }

// keygen makes a passphrase-less ed25519 key at path and returns its public key.
func keygen(ctx context.Context, r run.Runner, path, comment string) (string, error) {
	res, err := r.Run(ctx, run.Cmd{Name: "ssh-keygen", Args: []string{"-q", "-t", "ed25519", "-N", "", "-C", comment, "-f", path}})
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		return "", fmt.Errorf("ssh-keygen failed: %s", strings.TrimSpace(res.Stderr))
	}
	b, err := os.ReadFile(path + ".pub")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// NewBreakGlass generates a break-glass key for scope in a temporary
// directory, stores it in Bitwarden, deletes the local copy and returns the
// public key.
func NewBreakGlass(ctx context.Context, r run.Runner, scope string) (string, error) {
	return genAndStore(ctx, r, BreakGlassItem(scope), BreakGlassItem(scope))
}

// genAndStore generates a key in a temporary directory, stores it as item and
// returns the public key; the local copy is always deleted.
func genAndStore(ctx context.Context, r run.Runner, item, comment string) (string, error) {
	dir, err := os.MkdirTemp("", "athome-bg-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	key := filepath.Join(dir, "key")
	pub, err := keygen(ctx, r, key, comment)
	if err != nil {
		return "", err
	}
	priv, err := os.ReadFile(key)
	if err != nil {
		return "", err
	}
	if err := Store(ctx, r, item, SSHKeyPassword, strings.TrimRight(string(priv), "\n")); err != nil {
		return "", err
	}
	return pub, nil
}

// withItemKey writes the private key in item's notes to a 0600 temporary file
// for fn, then removes it.
func withItemKey(ctx context.Context, r run.Runner, item string, fn func(path string) error) error {
	res, err := r.Run(ctx, run.Cmd{Name: "rbw", Args: []string{"get", "--field", "notes", item}})
	if err != nil {
		return err
	}
	if res.Code != 0 || strings.TrimSpace(res.Stdout) == "" {
		return itemReadError(ctx, r, item, res)
	}
	dir, err := os.MkdirTemp("", "athome-bg-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "key")
	if err := os.WriteFile(path, []byte(strings.TrimRight(res.Stdout, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	return fn(path)
}

// BreakGlassPub reads scope's break-glass key from Bitwarden and returns its
// public key. A missing item is ErrNoItem.
func BreakGlassPub(ctx context.Context, r run.Runner, scope string) (string, error) {
	var pub string
	err := withItemKey(ctx, r, BreakGlassItem(scope), func(path string) error {
		res, err := r.Run(ctx, run.Cmd{Name: "ssh-keygen", Args: []string{"-y", "-f", path}})
		if err != nil {
			return err
		}
		if res.Code != 0 {
			return fmt.Errorf("ssh-keygen -y failed on %s: %s", BreakGlassItem(scope), strings.TrimSpace(res.Stderr))
		}
		pub = strings.TrimSpace(res.Stdout)
		return nil
	})
	return pub, err
}

// itemReadError tells a missing item (ErrNoItem) from rbw failing to read it,
// so a locked rbw is never mistaken for "this scope has no break-glass key".
func itemReadError(ctx context.Context, r run.Runner, item string, got run.Result) error {
	if u, err := r.Run(ctx, run.Cmd{Name: "rbw", Args: []string{"unlocked"}}); err != nil || u.Code != 0 {
		return ErrLocked
	}
	l, err := r.Run(ctx, run.Cmd{Name: "rbw", Args: []string{"list"}})
	if err != nil || l.Code != 0 {
		return fmt.Errorf("rbw list failed, so %s could not be read", item)
	}
	for _, line := range strings.Split(l.Stdout, "\n") {
		if strings.TrimSpace(line) == item {
			return fmt.Errorf("rbw get %s failed: %s", item, strings.TrimSpace(got.Stderr))
		}
	}
	return fmt.Errorf("%w: %s", ErrNoItem, item)
}

// runBreakGlass rotates one scope's break-glass key. The new key waits under
// the staging item until every box in the scope has it, so a skipped box stays
// reachable with the key under the real name.
func runBreakGlass(ctx context.Context, o Options, j *job, boxes []Box, save func() error) error {
	staging := stagingItem(j.Item)
	if j.NewPub == "" {
		o.say("==> break-glass key for %s: generating and storing %s", j.Scope, staging)
		pub, err := genAndStore(ctx, o.Run, staging, j.Item)
		if err != nil {
			return err
		}
		j.NewPub, j.Stored = pub, true
		if err := save(); err != nil {
			return err
		}
	}
	bs := boxesIn(o, j, boxes)
	boxStep(o, j, bs, []string{"", phaseSkipped}, phaseAdded, func(b Box) error { return b.Authorize(ctx, j.NewPub) })
	if err := save(); err != nil {
		return err
	}
	if anyAt(j, phaseAdded) {
		err := withItemKey(ctx, o.Run, staging, func(path string) error {
			boxStep(o, j, bs, []string{phaseAdded}, phaseChecked, func(b Box) error { return b.Login(ctx, path) })
			return nil
		})
		if err != nil {
			return err
		}
	}
	if err := save(); err != nil {
		return err
	}
	laptop, err := laptopPub(ctx, o)
	if err != nil {
		return err
	}
	boxStep(o, j, bs, []string{phaseChecked}, phaseRemoved, func(b Box) error {
		return b.Reset(ctx, []string{laptop, j.NewPub})
	})
	if err := save(); err != nil {
		return err
	}
	if len(skippedOf(j)) == 0 && !j.Promoted {
		err := promote(ctx, o, staging, j.Item)
		// A crash after the staging item was removed: the real item already has the new key.
		if errors.Is(err, ErrNoItem) {
			if pub, perr := BreakGlassPub(ctx, o.Run, j.Scope); perr == nil && pubBlob(pub) == pubBlob(j.NewPub) {
				err = nil
			}
		}
		if err != nil {
			return err
		}
		j.Promoted = true
		if err := save(); err != nil {
			return err
		}
		o.say("✓ break-glass key for %s rotated", j.Scope)
	}
	return nil
}

// managedSet is what a box's authorized_keys holds: the laptop key and its
// scope's break-glass key, when it has one. A pending break-glass job that has
// already put its new key on this box wins over the item, which still holds the
// old key until every box in the scope has the new one.
func managedSet(ctx context.Context, o Options, laptop string, b Box, jobs []*job) ([]string, error) {
	for _, j := range jobs {
		if j.Kind != kindBreakGlass || j.Scope != b.Scope() {
			continue
		}
		if p := j.Targets[boxTarget(b)]; p == phaseChecked || p == phaseRemoved {
			return []string{laptop, j.NewPub}, nil
		}
	}
	scope := b.Scope()
	bg, err := BreakGlassPub(ctx, o.Run, scope)
	if errors.Is(err, ErrNoItem) {
		return []string{laptop}, nil
	}
	if err != nil {
		return nil, err
	}
	return []string{laptop, bg}, nil
}

func laptopPub(ctx context.Context, o Options) (string, error) {
	key := LaptopKey(o.Home, o.Host)
	b, err := os.ReadFile(key + ".pub")
	if err != nil {
		return "", fmt.Errorf("no laptop public key at %s.pub", key)
	}
	return strings.TrimSpace(string(b)), nil
}

// anyAt reports whether some target of j is at phase.
func anyAt(j *job, phase string) bool {
	for _, p := range j.Targets {
		if p == phase {
			return true
		}
	}
	return false
}
