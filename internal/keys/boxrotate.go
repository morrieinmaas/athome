package keys

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/morrieinmaas/athome/internal/run"
)

const kindBox = "box"

func boxSkipTarget(name string) string { return "box:" + name }

// errBoxSkipped ends runBox after a box phase recorded the skip of its job.
var errBoxSkipped = errors.New("box skipped")

// boxRotation is one box's key rotation. Its phases run in order; each has a
// check (does it still have to run, from the job's saved state) and an apply.
type boxRotation struct {
	o         Options
	j         *job
	kb        KeyBox
	save      func() error
	protected map[string]bool
	gh        run.Runner // set by ghRunner, after the box's keys exist
}

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
	b := &boxRotation{o: o, j: j, kb: kb, save: save, protected: protectedBlobs(o)}
	for _, p := range []struct {
		todo  func() bool
		apply func(context.Context) error
	}{
		{b.needOld, b.readOld},
		{b.needNew, b.createNew},
		{b.always, b.refuseSameKey},
		{b.needAdd, b.addToGitHub},
		{b.needCheck, b.checkLogin},
		{b.needPromote, b.promote},
		{b.needRemoveOld, b.removeOld},
	} {
		if !p.todo() {
			continue
		}
		if err := p.apply(ctx); err != nil {
			if errors.Is(err, errBoxSkipped) {
				return nil
			}
			return err
		}
	}
	o.say("✓ %s key rotated", j.Item)
	return nil
}

func (b *boxRotation) always() bool { return true }

// skip records that this box's job is skipped with err, and ends runBox.
func (b *boxRotation) skip(err error) error {
	b.o.say("  skip %s: %v", b.j.Item, err)
	b.j.Targets[boxSkipTarget(b.j.Item)] = phaseSkipped
	if err := b.save(); err != nil {
		return err
	}
	return errBoxSkipped
}

// ghRunner is the runner for gh calls on the box's account; a failure to get
// its token stops the run rather than skipping the box.
func (b *boxRotation) ghRunner(ctx context.Context) (run.Runner, error) {
	if b.gh == nil {
		gh, err := GHTokenRunner(ctx, b.o.Run, b.kb.GitHubAccount())
		if err != nil {
			return nil, err
		}
		b.gh = gh
	}
	return b.gh, nil
}

func (b *boxRotation) needOld() bool { return b.j.OldPub == "" }

func (b *boxRotation) readOld(ctx context.Context) error {
	pub, err := b.kb.CurrentPub(ctx)
	if err != nil {
		return b.skip(err)
	}
	pub = strings.TrimSpace(pub)
	if err := checkBoxPub("current key", pub, b.protected); err != nil {
		return b.skip(err)
	}
	b.j.OldPub = pub
	return b.save()
}

func (b *boxRotation) needNew() bool { return b.j.NewPub == "" }

func (b *boxRotation) createNew(ctx context.Context) error {
	b.o.say("==> %s: creating a new key on the box", b.j.Item)
	pub, err := b.kb.NewKey(ctx)
	if err != nil {
		return b.skip(err)
	}
	pub = strings.TrimSpace(pub)
	if err := checkBoxPub("new key", pub, b.protected); err != nil {
		return b.skip(err)
	}
	b.j.NewPub = pub
	return b.save()
}

// refuseSameKey: the old key's GitHub entries are deleted at the end, so a new
// key that is the old one would leave the box with no registered key.
func (b *boxRotation) refuseSameKey(context.Context) error {
	if pubBlob(b.j.NewPub) == pubBlob(b.j.OldPub) {
		return b.skip(errors.New("the box's new key is its current key; refusing, so its only registered key is never removed"))
	}
	return nil
}

func (b *boxRotation) needAdd() bool { return b.j.Targets["github"] == "" }

func (b *boxRotation) addToGitHub(ctx context.Context) error {
	gh, err := b.ghRunner(ctx)
	if err != nil {
		return err
	}
	b.o.say("==> %s: adding its key to GitHub", b.j.Item)
	if err := WithPubFile(b.j.NewPub, func(path string) error { return ghAdd(ctx, gh, path, "athome-"+b.j.Item) }); err != nil {
		return err
	}
	b.j.Targets["github"] = phaseAdded
	return b.save()
}

func (b *boxRotation) needCheck() bool { return b.j.Targets["github"] == phaseAdded }

func (b *boxRotation) checkLogin(ctx context.Context) error {
	b.o.say("==> %s: checking a GitHub login with the new key", b.j.Item)
	if err := b.kb.CheckGitHub(ctx, true); err != nil {
		return b.skip(err)
	}
	b.j.Targets["github"] = phaseChecked
	return b.save()
}

func (b *boxRotation) needPromote() bool { return !b.j.Promoted }

func (b *boxRotation) promote(ctx context.Context) error {
	b.o.say("==> %s: promoting the new key on the box", b.j.Item)
	if err := b.kb.PromoteKey(ctx, b.j.NewPub); err != nil {
		return b.skip(err)
	}
	b.j.Promoted = true
	return b.save()
}

func (b *boxRotation) needRemoveOld() bool { return b.j.Targets["github"] == phaseChecked }

func (b *boxRotation) removeOld(ctx context.Context) error {
	gh, err := b.ghRunner(ctx)
	if err != nil {
		return err
	}
	b.o.say("==> %s: removing the old key from GitHub", b.j.Item)
	if err := ghRemoveTitled(ctx, gh, b.j.OldPub, map[string]string{
		"user/keys":             "athome-" + b.j.Item,
		"user/ssh_signing_keys": "athome-" + b.j.Item + "-sign",
	}); err != nil {
		return err
	}
	b.j.Targets["github"] = phaseRemoved
	return b.save()
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
