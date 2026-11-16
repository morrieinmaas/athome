package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
)

type Cmd struct {
	Name  string
	Args  []string
	Dir   string
	Env   []string
	Stdin string
}

type Result struct {
	Stdout string
	Stderr string
	Code   int
}

// Runner is the only way steps reach external tools. A non-zero exit is a
// Result; the error is reserved for a process that cannot start.
type Runner interface {
	Run(ctx context.Context, c Cmd) (Result, error)
	LookPath(name string) (string, error)
}

type execRunner struct{}

func Exec() Runner { return execRunner{} }

func (execRunner) LookPath(name string) (string, error) { return exec.LookPath(name) }

func (execRunner) Run(ctx context.Context, c Cmd) (Result, error) {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Dir = c.Dir
	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}
	if c.Stdin != "" {
		cmd.Stdin = strings.NewReader(c.Stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := cmd.Run()
	res := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && ctx.Err() == nil {
		res.Code = exitErr.ExitCode()
		return res, nil
	}
	if err != nil {
		return res, fmt.Errorf("run %s: %w", c.Name, err)
	}
	return res, nil
}

type rule struct {
	name string
	args []string
	res  Result
}

// Fake records every call and answers from rules matched on exact name and
// args. The last matching rule wins; an unmatched call returns a zero Result.
type Fake struct {
	mu      sync.Mutex
	calls   []Cmd
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

func (f *Fake) Return(r Result) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pending != nil {
		f.pending.res = r
	}
}

func (f *Fake) Calls() []Cmd {
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

func (f *Fake) Run(ctx context.Context, c Cmd) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
	for i := len(f.rules) - 1; i >= 0; i-- {
		r := f.rules[i]
		if r.name == c.Name && slices.Equal(r.args, c.Args) {
			return r.res, nil
		}
	}
	return Result{}, nil
}

func (f *Fake) LookPath(name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.have[name] {
		return "/fake/bin/" + name, nil
	}
	return "", fmt.Errorf("%s: %w", name, exec.ErrNotFound)
}
