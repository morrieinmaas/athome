package keys

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/morrieinmaas/athome/internal/run"
)

// Options is what a rotation needs. Host is the short hostname init names the
// key after; Now is time.Now outside tests.
type Options struct {
	Home string
	Host string
	Run  run.Runner
	Out  io.Writer
	Now  func() time.Time
}

func (o Options) say(format string, a ...any) {
	if o.Out != nil {
		fmt.Fprintf(o.Out, format+"\n", a...)
	}
}

// Kinds picks what Rotate rotates; Boxes is the box keys.
type Kinds struct{ Laptop, Boxes, BreakGlass bool }

// IncompleteError lists the boxes a run skipped; their old key stays until
// `athome keys rotate --resume` reaches them.
type IncompleteError struct{ Skipped []string }

func (e *IncompleteError) Error() string {
	return "skipped: " + strings.Join(e.Skipped, ", ") + "; run athome keys rotate --resume once they are reachable"
}

// Rotate starts a rotation of the chosen kinds, in the order laptop, boxes,
// break-glass. It refuses while an earlier rotation is unfinished.
func Rotate(ctx context.Context, o Options, k Kinds) error {
	unlock, err := lockState(o.Home)
	if err != nil {
		return err
	}
	defer unlock()
	st, err := loadState(o.Home)
	if err != nil {
		return err
	}
	if len(st.Jobs) > 0 {
		return fmt.Errorf("a rotation is unfinished (%s); run athome keys rotate --resume", st.Jobs[0].Kind)
	}
	boxes, err := Inventory(o.Home)
	if err != nil {
		return err
	}
	if k.Laptop {
		j, err := newLaptopJob(ctx, o)
		if err != nil {
			return err
		}
		st.Jobs = append(st.Jobs, j)
	}
	if k.Boxes {
		if len(boxes) > 0 {
			// ponytail: box keys are created on the box, which is the remote-machines build's ssh work.
			return errors.New("rotating box keys arrives with athome machine (the remote-machines build)")
		}
		o.say("no machines in the inventory: no box keys to rotate")
	}
	if k.BreakGlass {
		scopes := scopesOf(boxes)
		if len(scopes) == 0 {
			o.say("no scopes in the inventory: no break-glass keys to rotate")
		}
		for _, s := range scopes {
			st.Jobs = append(st.Jobs, &job{Kind: kindBreakGlass, Scope: s, Item: BreakGlassItem(s), Targets: map[string]string{}})
		}
	}
	if len(st.Jobs) == 0 {
		return nil
	}
	if err := st.save(o.Home); err != nil {
		return err
	}
	return runJobs(ctx, o, st, boxes)
}

// Resume finishes the jobs an earlier run left, retrying skipped boxes.
func Resume(ctx context.Context, o Options) error {
	unlock, err := lockState(o.Home)
	if err != nil {
		return err
	}
	defer unlock()
	st, err := loadState(o.Home)
	if err != nil {
		return err
	}
	if len(st.Jobs) == 0 {
		o.say("nothing to resume")
		return nil
	}
	boxes, err := Inventory(o.Home)
	if err != nil {
		return err
	}
	return runJobs(ctx, o, st, boxes)
}

func scopesOf(boxes []Box) []string {
	var out []string
	for _, b := range boxes {
		if !slices.Contains(out, b.Scope()) {
			out = append(out, b.Scope())
		}
	}
	return out
}

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

// runJobs runs every job in order, saving after each step. Done jobs are
// dropped; a job with skipped boxes stays for --resume.
func runJobs(ctx context.Context, o Options, st *state, boxes []Box) error {
	// Every job reads or writes Bitwarden; a locked rbw must stop the run, not skip boxes.
	if res, err := o.Run.Run(ctx, run.Cmd{Name: "rbw", Args: []string{"unlocked"}}); err != nil || res.Code != 0 {
		return ErrLocked
	}
	var skipped []string
	var left []*job
	for _, j := range st.Jobs {
		save := func() error { return st.save(o.Home) }
		var err error
		switch j.Kind {
		case kindLaptop:
			err = runLaptop(ctx, o, j, boxes, save)
		case kindBreakGlass:
			err = runBreakGlass(ctx, o, j, boxes, save)
		default:
			err = fmt.Errorf("unknown job kind %q in %s", j.Kind, StatePath(o.Home))
		}
		if err != nil {
			return err
		}
		if s := skippedOf(j); len(s) > 0 {
			skipped = append(skipped, s...)
			left = append(left, j)
		}
	}
	st.Jobs = left
	if len(left) == 0 {
		now := time.Now
		if o.Now != nil {
			now = o.Now
		}
		st.LastRotated = now().UTC().Format(time.RFC3339)
	}
	if err := st.save(o.Home); err != nil {
		return err
	}
	if len(skipped) > 0 {
		return &IncompleteError{Skipped: skipped}
	}
	return nil
}

func skippedOf(j *job) []string {
	var out []string
	for t, p := range j.Targets {
		if p != phaseRemoved {
			out = append(out, strings.TrimPrefix(t, "box:"))
		}
	}
	slices.Sort(out)
	return out
}

func boxTarget(b Box) string { return "box:" + b.Name() }

// boxesIn returns the boxes a job reaches: every box for the laptop key, the
// scope's boxes for a break-glass key. Each gets a target entry.
func boxesIn(o Options, j *job, boxes []Box) []Box {
	var out []Box
	var names []string
	for _, b := range boxes {
		if j.Kind == kindBreakGlass && b.Scope() != j.Scope {
			continue
		}
		if _, ok := j.Targets[boxTarget(b)]; !ok {
			j.Targets[boxTarget(b)] = ""
		}
		out = append(out, b)
		names = append(names, boxTarget(b))
	}
	// A box that left the inventory has nothing left to rotate.
	for t := range j.Targets {
		if strings.HasPrefix(t, "box:") && !slices.Contains(names, t) {
			o.say("  %s is no longer in the inventory; dropped from this rotation", strings.TrimPrefix(t, "box:"))
			delete(j.Targets, t)
		}
	}
	return out
}

// boxStep runs fn on every box whose phase is one of from, moving it to to on
// success and to skipped on failure.
func boxStep(o Options, j *job, boxes []Box, from []string, to string, fn func(Box) error) {
	for _, b := range boxes {
		t := boxTarget(b)
		if !slices.Contains(from, j.Targets[t]) {
			continue
		}
		if err := fn(b); err != nil {
			o.say("  skip %s: %v", b.Name(), err)
			j.Targets[t] = phaseSkipped
			continue
		}
		j.Targets[t] = to
	}
}

// stagingItem holds a new key in Bitwarden until it is in use everywhere, so
// the item under the real name keeps the key that still works.
func stagingItem(item string) string { return item + ".rotate" }

// runLaptop rotates the laptop key: generate, store, add everywhere, check
// everywhere, and only then swap the files and remove the old key.
func runLaptop(ctx context.Context, o Options, j *job, boxes []Box, save func() error) error {
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
	if err := laptopRemoveOld(ctx, o, j, bs, save); err != nil {
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
	// A crash between the file swap and saving Installed leaves the new key in place.
	if !j.Installed && !exists(staged) && pubBlob(readTrim(j.Key+".pub")) == pubBlob(j.NewPub) {
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
		if err := promote(ctx, o, stagingItem(j.Item), j.Item); err != nil {
			return err
		}
		j.Promoted = true
		return save()
	}
	return nil
}

func laptopRemoveOld(ctx context.Context, o Options, j *job, bs []Box, save func() error) error {
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
		managed, err := managedSet(ctx, o, j.NewPub, b.Scope())
		if err != nil {
			return err
		}
		return b.Reset(ctx, managed)
	})
	return save()
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
	err := withItemKey(ctx, o.Run, staging, func(path string) error {
		boxStep(o, j, bs, []string{phaseAdded}, phaseChecked, func(b Box) error { return b.Login(ctx, path) })
		return nil
	})
	if err != nil {
		return err
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
	if len(skippedOf(j)) == 0 {
		if err := promote(ctx, o, staging, j.Item); err != nil {
			return err
		}
		j.Promoted = true
		o.say("✓ break-glass key for %s rotated", j.Scope)
	}
	return nil
}

// promote copies the staging item over the real one and removes the staging
// item. A failed removal is only a note: the real item is already right.
func promote(ctx context.Context, o Options, staging, item string) error {
	err := withItemKey(ctx, o.Run, staging, func(path string) error {
		priv, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return Store(ctx, o.Run, item, SSHKeyPassword, strings.TrimRight(string(priv), "\n"))
	})
	if err != nil {
		return err
	}
	if res, err := o.Run.Run(ctx, run.Cmd{Name: "rbw", Args: []string{"remove", staging}}); err != nil || res.Code != 0 {
		o.say("  note: could not remove %s from Bitwarden; remove it by hand (rbw remove %s)", staging, staging)
	}
	return nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func readTrim(path string) string {
	b, _ := os.ReadFile(path)
	return strings.TrimSpace(string(b))
}

// managedSet is what a box's authorized_keys holds: the laptop key and its
// scope's break-glass key, when it has one.
func managedSet(ctx context.Context, o Options, laptop, scope string) ([]string, error) {
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
