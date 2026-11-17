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

func newLaptopJob(ctx context.Context, o Options) (*job, error) {
	key := LaptopKey(o.Home, o.Host)
	if _, err := os.Stat(key); err != nil {
		return nil, fmt.Errorf("no laptop key at %s; athome init creates one", key)
	}
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		res, rerr := o.Run.Run(ctx, run.Cmd{Name: "ssh-keygen", Args: []string{"-y", "-f", key}})
		if rerr != nil || res.Code != 0 {
			return nil, fmt.Errorf("no %s.pub and ssh-keygen -y could not derive it", key)
		}
		pub = []byte(res.Stdout)
	}
	return &job{Kind: kindLaptop, Item: ItemFor(key), Key: key, OldPub: strings.TrimSpace(string(pub)), Targets: map[string]string{"github": ""}}, nil
}

// runLaptop rotates the laptop key: generate, store, add everywhere, check
// everywhere, and only then swap the files and remove the old key.
func runLaptop(ctx context.Context, o Options, j *job, boxes []Box, jobs []*job, save func() error) error {
	newKey, err := laptopNewKey(ctx, o, j, save)
	if err != nil {
		return err
	}
	bs := boxesIn(o, j, boxes)
	if err := laptopAdd(ctx, o, j, bs, newKey, save); err != nil {
		return err
	}
	if err := laptopCheck(ctx, o, j, bs, newKey, save); err != nil {
		return err
	}
	if err := laptopInstall(ctx, o, j, save); err != nil {
		return err
	}
	if err := laptopRemoveOld(ctx, o, j, bs, jobs, save); err != nil {
		return err
	}
	if len(skippedOf(j)) == 0 {
		os.Remove(j.Key + ".old")
		os.Remove(j.Key + ".old.pub")
		o.say("✓ laptop key rotated")
	}
	return nil
}

// laptopNewKey generates the new key once and stores it under the staging
// item. It returns where the new private key is now.
func laptopNewKey(ctx context.Context, o Options, j *job, save func() error) (string, error) {
	staged := j.Key + ".rotate"
	if j.NewPub == "" {
		o.say("==> laptop key: generating a new key")
		os.Remove(staged)
		os.Remove(staged + ".pub")
		pub, err := keygen(ctx, o.Run, staged, commentOf(j.OldPub, o.Host))
		if err != nil {
			return "", err
		}
		j.NewPub = pub
		if err := save(); err != nil {
			return "", err
		}
	}
	// A crash inside or after the file swap leaves the new key at j.Key: finish
	// the swap instead of asking for a fresh start.
	if !j.Installed && !exists(staged) && derivedBlob(ctx, o, j.Key) == pubBlob(j.NewPub) {
		if exists(staged + ".pub") {
			if err := os.Rename(staged+".pub", j.Key+".pub"); err != nil {
				return "", err
			}
		}
		if err := os.Chmod(j.Key, 0o600); err != nil {
			return "", err
		}
		reapplySigners(ctx, o)
		j.Installed = true
	}
	newKey := staged
	if j.Installed {
		newKey = j.Key
	}
	if !exists(newKey) {
		return "", fmt.Errorf("the new key %s is missing; remove %s to start over", newKey, StatePath(o.Home))
	}
	if !j.Stored {
		o.say("==> laptop key: storing it in Bitwarden as %s", stagingItem(j.Item))
		priv, err := os.ReadFile(newKey)
		if err != nil {
			return "", err
		}
		if err := Store(ctx, o.Run, stagingItem(j.Item), SSHKeyPassword, strings.TrimRight(string(priv), "\n")); err != nil {
			return "", err
		}
		j.Stored = true
		if err := save(); err != nil {
			return "", err
		}
	}
	return newKey, nil
}

func laptopAdd(ctx context.Context, o Options, j *job, bs []Box, newKey string, save func() error) error {
	if j.Targets["github"] == "" {
		o.say("==> laptop key: adding it to GitHub")
		if err := ghAdd(ctx, o.Run, newKey+".pub", o.Host); err != nil {
			return err
		}
		j.Targets["github"] = phaseAdded
		if err := save(); err != nil {
			return err
		}
	}
	boxStep(o, j, bs, []string{"", phaseSkipped}, phaseAdded, func(b Box) error { return b.Authorize(ctx, j.NewPub) })
	return save()
}

func laptopCheck(ctx context.Context, o Options, j *job, bs []Box, newKey string, save func() error) error {
	if j.Targets["github"] == phaseAdded {
		o.say("==> laptop key: checking a GitHub login with the new key")
		if err := ghLogin(ctx, o.Run, newKey); err != nil {
			return err
		}
		j.Targets["github"] = phaseChecked
		if err := save(); err != nil {
			return err
		}
	}
	boxStep(o, j, bs, []string{phaseAdded}, phaseChecked, func(b Box) error { return b.Login(ctx, newKey) })
	return save()
}

// laptopInstall swaps the key files and moves the new key from the staging
// item to the real one, now that GitHub accepts it.
func laptopInstall(ctx context.Context, o Options, j *job, save func() error) error {
	if !j.Installed {
		o.say("==> laptop key: installing it as %s", j.Key)
		if err := swapIn(j.Key); err != nil {
			return err
		}
		j.Installed = true
		if err := save(); err != nil {
			return err
		}
		reapplySigners(ctx, o)
	}
	if !j.Promoted {
		o.say("==> laptop key: storing it in Bitwarden as %s", j.Item)
		// From the installed file, not the staging item: a crash after the
		// staging item was removed must not block the retry.
		priv, err := os.ReadFile(j.Key)
		if err != nil {
			return err
		}
		if err := Store(ctx, o.Run, j.Item, SSHKeyPassword, strings.TrimRight(string(priv), "\n")); err != nil {
			return err
		}
		removeItem(ctx, o, stagingItem(j.Item))
		j.Promoted = true
		return save()
	}
	return nil
}

func laptopRemoveOld(ctx context.Context, o Options, j *job, bs []Box, jobs []*job, save func() error) error {
	if j.Targets["github"] == phaseChecked {
		o.say("==> laptop key: removing the old key from GitHub")
		if err := ghRemove(ctx, o.Run, j.OldPub); err != nil {
			return err
		}
		j.Targets["github"] = phaseRemoved
		if err := save(); err != nil {
			return err
		}
	}
	boxStep(o, j, bs, []string{phaseChecked}, phaseRemoved, func(b Box) error {
		managed, err := managedSet(ctx, o, j.NewPub, b, jobs)
		if err != nil {
			return err
		}
		return b.Reset(ctx, managed)
	})
	return save()
}

// swapIn moves key to key.old and key.rotate to key, each with its .pub.
func swapIn(key string) error {
	for _, mv := range [][2]string{
		{key, key + ".old"}, {key + ".pub", key + ".old.pub"},
		{key + ".rotate", key}, {key + ".rotate.pub", key + ".pub"},
	} {
		if err := os.Rename(mv[0], mv[1]); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return os.Chmod(key, 0o600)
}

// reapplySigners re-renders allowed_signers from the new public key.
func reapplySigners(ctx context.Context, o Options) {
	if _, err := o.Run.LookPath("chezmoi"); err != nil {
		o.say("  note: chezmoi not found; run chezmoi apply to refresh ~/.config/git/allowed_signers")
		return
	}
	target := filepath.Join(o.Home, ".config", "git", "allowed_signers")
	if res, err := o.Run.Run(ctx, run.Cmd{Name: "chezmoi", Args: []string{"apply", target}}); err != nil || res.Code != 0 {
		o.say("  note: chezmoi apply %s failed; run it by hand", target)
	}
}

// commentOf keeps the old key's comment, else names the host.
func commentOf(pub, host string) string {
	if f := strings.Fields(pub); len(f) >= 3 {
		return strings.Join(f[2:], " ")
	}
	return "athome@" + host
}
