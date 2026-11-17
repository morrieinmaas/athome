// Package runtest is a recording fake for run.Runner, for tests only.
package runtest

import (
	"context"
	"fmt"
	"os/exec"
	"slices"
	"sync"

	"github.com/morrieinmaas/athome/internal/run"
)

type rule struct {
	name string
	args []string
	res  run.Result
}

// Fake records every call and answers from rules matched on exact name and
// args. The last matching rule wins; an unmatched call returns a zero run.Result.
type Fake struct {
	mu      sync.Mutex
	calls   []run.Cmd
	rules   []*rule
	pending *rule
	have    map[string]bool
}

func NewFake() *Fake { return &Fake{have: map[string]bool{}} }

// On starts a rule; call Return to set its result.
func (f *Fake) On(name string, args ...string) *Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending = &rule{name: name, args: slices.Clone(args)}
	f.rules = append(f.rules, f.pending)
	return f
}

func (f *Fake) Return(r run.Result) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pending != nil {
		f.pending.res = r
	}
}

func (f *Fake) Calls() []run.Cmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *Fake) Have(names ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range names {
		f.have[n] = true
	}
}

func (f *Fake) Run(ctx context.Context, c run.Cmd) (run.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
	for i := len(f.rules) - 1; i >= 0; i-- {
		r := f.rules[i]
		if r.name == c.Name && slices.Equal(r.args, c.Args) {
			return r.res, nil
		}
	}
	return run.Result{}, nil
}

func (f *Fake) LookPath(name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.have[name] {
		return "/fake/bin/" + name, nil
	}
	return "", fmt.Errorf("%s: %w", name, exec.ErrNotFound)
}
