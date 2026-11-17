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
	dir, err := os.MkdirTemp("", "athome-bg-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	key := filepath.Join(dir, "key")
	pub, err := keygen(ctx, r, key, BreakGlassItem(scope))
	if err != nil {
		return "", err
	}
	priv, err := os.ReadFile(key)
	if err != nil {
		return "", err
	}
	if err := Store(ctx, r, BreakGlassItem(scope), SSHKeyPassword, strings.TrimRight(string(priv), "\n")); err != nil {
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
		return fmt.Errorf("%w: %s", ErrNoItem, item)
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
