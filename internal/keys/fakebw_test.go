package keys

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/run/runtest"
)

// fakeBW is a stateful rbw in front of a runtest.Fake. add and edit run the
// EDITOR script the way rbw does, so the stored content is what Store wrote.
// Every other command goes to the Fake, except ssh-keygen, which runs for real
// (its paths are always under a test temp dir).
type fakeBW struct {
	*runtest.Fake
	t         *testing.T
	mu        sync.Mutex
	locked    bool
	items     map[string][]string // name -> contents, one per item
	bwCalls   []string
	keygenOK  bool
	listFails bool
}

func newFakeBW(t *testing.T) *fakeBW {
	return &fakeBW{Fake: runtest.NewFake(), t: t, items: map[string][]string{}, keygenOK: true}
}

func (f *fakeBW) put(name, content string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items[name] = append(f.items[name], content)
}

func (f *fakeBW) get(name string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.items[name])
}

func (f *fakeBW) Run(ctx context.Context, c run.Cmd) (run.Result, error) {
	if c.Name == "ssh-keygen" && f.keygenOK {
		f.Fake.Run(ctx, c) // record it
		out, err := exec.CommandContext(ctx, "ssh-keygen", c.Args...).Output()
		if err != nil {
			code := 1
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			}
			return run.Result{Code: code}, nil
		}
		return run.Result{Stdout: string(out)}, nil
	}
	if c.Name != "rbw" {
		return f.Fake.Run(ctx, c)
	}
	f.Fake.Run(ctx, c)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bwCalls = append(f.bwCalls, strings.Join(c.Args, " "))
	if len(c.Args) == 0 {
		return run.Result{Code: 2}, nil
	}
	switch c.Args[0] {
	case "unlocked":
		if f.locked {
			return run.Result{Code: 1}, nil
		}
		return run.Result{}, nil
	case "list":
		if f.listFails {
			return run.Result{Code: 1, Stderr: "boom"}, nil
		}
		var names []string
		for n, items := range f.items {
			for range items {
				names = append(names, n)
			}
		}
		slices.Sort(names)
		return run.Result{Stdout: strings.Join(names, "\n") + "\n"}, nil
	case "get":
		args := c.Args[1:]
		field := ""
		if len(args) >= 2 && args[0] == "--field" {
			field, args = args[1], args[2:]
		}
		if len(args) != 1 || len(f.items[args[0]]) != 1 {
			return run.Result{Code: 1, Stderr: "no such entry"}, nil
		}
		pw, notes, _ := strings.Cut(f.items[args[0]][0], "\n")
		if field == "notes" {
			return run.Result{Stdout: notes}, nil
		}
		return run.Result{Stdout: pw + "\n"}, nil
	case "add", "edit":
		name := c.Args[1]
		var editor string
		for _, e := range c.Env {
			if v, ok := strings.CutPrefix(e, "EDITOR="); ok {
				editor = v
			}
		}
		tmp := filepath.Join(f.t.TempDir(), "edit")
		if err := exec.Command("sh", editor, tmp).Run(); err != nil {
			return run.Result{Code: 1, Stderr: err.Error()}, nil
		}
		b, err := os.ReadFile(tmp)
		if err != nil {
			return run.Result{Code: 1}, nil
		}
		if c.Args[0] == "add" {
			f.items[name] = append(f.items[name], string(b))
		} else {
			if len(f.items[name]) != 1 {
				return run.Result{Code: 1, Stderr: "no single entry"}, nil
			}
			f.items[name][0] = string(b)
		}
		return run.Result{}, nil
	}
	return run.Result{Code: 2}, nil
}
