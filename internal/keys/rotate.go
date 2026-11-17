package keys

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
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
	if err := requireUnlocked(ctx, o); err != nil {
		return err
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
	if err := requireUnlocked(ctx, o); err != nil {
		return err
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

// runJobs runs every job in order, saving after each step. Done jobs are
// dropped; a job with skipped boxes stays for --resume.
func runJobs(ctx context.Context, o Options, st *state, boxes []Box) error {
	var skipped []string
	var left []*job
	for _, j := range st.Jobs {
		if jobDone(j) {
			continue
		}
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
			st.dropDone()
			st.save(o.Home)
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
	removeItem(ctx, o, staging)
	return nil
}

func removeItem(ctx context.Context, o Options, item string) {
	if res, err := o.Run.Run(ctx, run.Cmd{Name: "rbw", Args: []string{"remove", item}}); err != nil || res.Code != 0 {
		o.say("  note: could not remove %s from Bitwarden; remove it by hand (rbw remove %s)", item, item)
	}
}

// derivedBlob is the public key blob of a private key file, "" if unreadable.
func derivedBlob(ctx context.Context, o Options, key string) string {
	if !exists(key) {
		return ""
	}
	res, err := o.Run.Run(ctx, run.Cmd{Name: "ssh-keygen", Args: []string{"-y", "-f", key}})
	if err != nil || res.Code != 0 {
		return ""
	}
	return pubBlob(res.Stdout)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// requireUnlocked stops a run before any job is saved or touched: every job
// reads or writes Bitwarden, and a locked rbw must never look like a missing item.
func requireUnlocked(ctx context.Context, o Options) error {
	if res, err := o.Run.Run(ctx, run.Cmd{Name: "rbw", Args: []string{"unlocked"}}); err != nil || res.Code != 0 {
		return ErrLocked
	}
	return nil
}

// jobDone: every target removed and the new key under the real item name.
func jobDone(j *job) bool {
	return j.Promoted && len(skippedOf(j)) == 0
}

// dropDone keeps only unfinished jobs, so a later job's failure does not make
// --resume redo finished ones.
func (s *state) dropDone() {
	var left []*job
	for _, j := range s.Jobs {
		if !jobDone(j) {
			left = append(left, j)
		}
	}
	s.Jobs = left
}
