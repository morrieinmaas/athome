package keys

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/morrieinmaas/athome/internal/run"
)

// ghEnvRunner adds env to every command, so a box's GitHub account token
// reaches gh without ever being an argument.
type ghEnvRunner struct {
	run.Runner
	env []string
}

func (r ghEnvRunner) Run(ctx context.Context, c run.Cmd) (run.Result, error) {
	c.Env = append(append([]string(nil), c.Env...), r.env...)
	return r.Runner.Run(ctx, c)
}

// ghRunnerFor is o.Run, or o.Run with GH_TOKEN of the box's account.
func ghRunnerFor(ctx context.Context, o Options, b KeyBox) (run.Runner, error) {
	account := b.GitHubAccount()
	if account == "" {
		return o.Run, nil
	}
	res, err := o.Run.Run(ctx, run.Cmd{Name: "gh", Args: []string{"auth", "token", "--hostname", "github.com", "--user", account}})
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(res.Stdout)
	if res.Code != 0 || token == "" {
		return nil, fmt.Errorf("gh has no token for GitHub account %s (run: gh auth login)", account)
	}
	return ghEnvRunner{o.Run, []string{"GH_TOKEN=" + token}}, nil
}

const kindBox = "box"

func boxSkipTarget(name string) string { return "box:" + name }

// runBox rotates one box's key. A box call that fails skips the job; a gh
// failure stops the run, as for the laptop key.
func runBox(ctx context.Context, o Options, j *job, boxes []Box, save func() error) error {
	var kb KeyBox
	for _, b := range boxes {
		if k, ok := b.(KeyBox); ok && b.Name() == j.Item {
			kb = k
		}
	}
	if kb == nil {
		o.say("  %s is no longer in the inventory; dropped from this rotation", j.Item)
		j.Targets = map[string]string{"github": phaseRemoved}
		j.Promoted = true
		return save()
	}
	delete(j.Targets, boxSkipTarget(j.Item))
	skip := func(err error) error {
		o.say("  skip %s: %v", j.Item, err)
		j.Targets[boxSkipTarget(j.Item)] = phaseSkipped
		return save()
	}
	protected := protectedBlobs(o)
	if j.OldPub == "" {
		pub, err := kb.CurrentPub(ctx)
		if err != nil {
			return skip(err)
		}
		pub = strings.TrimSpace(pub)
		if err := checkBoxPub("current key", pub, protected); err != nil {
			return skip(err)
		}
		j.OldPub = pub
		if err := save(); err != nil {
			return err
		}
	}
	if j.NewPub == "" {
		o.say("==> %s: creating a new key on the box", j.Item)
		pub, err := kb.NewKey(ctx)
		if err != nil {
			return skip(err)
		}
		pub = strings.TrimSpace(pub)
		if err := checkBoxPub("new key", pub, protected); err != nil {
			return skip(err)
		}
		j.NewPub = pub
		if err := save(); err != nil {
			return err
		}
	}
	gh, err := ghRunnerFor(ctx, o, kb)
	if err != nil {
		return err
	}
	if j.Targets["github"] == "" {
		o.say("==> %s: adding its key to GitHub", j.Item)
		if err := ghAddPub(ctx, gh, j.NewPub, "athome-"+j.Item); err != nil {
			return err
		}
		j.Targets["github"] = phaseAdded
		if err := save(); err != nil {
			return err
		}
	}
	if j.Targets["github"] == phaseAdded {
		o.say("==> %s: checking a GitHub login with the new key", j.Item)
		if err := kb.CheckGitHub(ctx, true); err != nil {
			return skip(err)
		}
		j.Targets["github"] = phaseChecked
		if err := save(); err != nil {
			return err
		}
	}
	if !j.Promoted {
		o.say("==> %s: promoting the new key on the box", j.Item)
		if err := kb.PromoteKey(ctx, j.NewPub); err != nil {
			return skip(err)
		}
		j.Promoted = true
		if err := save(); err != nil {
			return err
		}
	}
	if j.Targets["github"] == phaseChecked {
		o.say("==> %s: removing the old key from GitHub", j.Item)
		if err := ghRemoveTitled(ctx, gh, j.OldPub, map[string]string{
			"user/keys":             "athome-" + j.Item,
			"user/ssh_signing_keys": "athome-" + j.Item + "-sign",
		}); err != nil {
			return err
		}
		j.Targets["github"] = phaseRemoved
		if err := save(); err != nil {
			return err
		}
	}
	o.say("✓ %s key rotated", j.Item)
	return nil
}

var boxPubLine = regexp.MustCompile(`^ssh-ed25519 [A-Za-z0-9+/]+=*( [^\r\n]*)?$`)

// protectedBlobs are the public keys a box may never claim as its own: the
// laptop key, whose GitHub entries a rotation must not delete.
func protectedBlobs(o Options) map[string]bool {
	out := map[string]bool{}
	if b, err := os.ReadFile(LaptopKey(o.Home, o.Host) + ".pub"); err == nil {
		if blob := pubBlob(string(b)); blob != "" {
			out[blob] = true
		}
	}
	return out
}

// checkBoxPub accepts only one "ssh-ed25519 <base64>[ comment]" line that is
// not a protected key. A box is not trusted to name the laptop's key.
func checkBoxPub(what, pub string, protected map[string]bool) error {
	if !boxPubLine.MatchString(pub) {
		return fmt.Errorf("the box's %s is not one ssh-ed25519 public key line", what)
	}
	if protected[pubBlob(pub)] {
		return fmt.Errorf("the box's %s is the laptop key; refusing", what)
	}
	return nil
}

// ghAddPub writes pub to a temp file for gh ssh-key add and removes it again.
func ghAddPub(ctx context.Context, r run.Runner, pub, title string) error {
	f, err := os.CreateTemp("", "athome-box-*.pub")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(pub + "\n"); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return ghAdd(ctx, r, f.Name(), title)
}

// boxJobs is one job per box that can rotate its own key.
func boxJobs(o Options, boxes []Box) []*job {
	var out []*job
	for _, b := range boxes {
		if _, ok := b.(KeyBox); !ok {
			o.say("  %s cannot rotate its own key; skipped", b.Name())
			continue
		}
		out = append(out, &job{Kind: kindBox, Scope: b.Scope(), Item: b.Name(), Targets: map[string]string{"github": ""}})
	}
	return out
}
