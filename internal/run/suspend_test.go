package run_test

import (
	"context"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
)

type stub struct{ onRun func() }

func (s stub) Run(context.Context, run.Cmd) (run.Result, error) { s.onRun(); return run.Result{}, nil }
func (stub) LookPath(name string) (string, error)               { return "/bin/" + name, nil }

func TestSuspending(t *testing.T) {
	for _, c := range []struct {
		name   string
		cmd    run.Cmd
		around bool
	}{
		{"plain", run.Cmd{Name: "x"}, false},
		{"interactive", run.Cmd{Name: "x", Interactive: true}, true},
		{"sudo", run.Cmd{Name: "x", NeedsSudo: true}, true},
		{"sudo without -n", run.Cmd{Name: "sudo", Args: []string{"dnf", "install", "-y", "jq"}}, true},
		{"bare sudo", run.Cmd{Name: "sudo"}, true},
		{"sudo -n", run.Cmd{Name: "sudo", Args: []string{"-n", "true"}}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			inAround, ranInside, calls := false, false, 0
			around := func(f func()) { calls++; inAround = true; f(); inAround = false }
			r := run.Suspending(stub{func() { ranInside = inAround }}, around)
			if _, err := r.Run(context.Background(), c.cmd); err != nil {
				t.Fatal(err)
			}
			if (calls == 1) != c.around || ranInside != c.around {
				t.Errorf("around calls=%d ranInside=%v, want around=%v", calls, ranInside, c.around)
			}
		})
	}
}

func TestSuspendingLookPath(t *testing.T) {
	r := run.Suspending(stub{}, func(f func()) { f() })
	if p, err := r.LookPath("rbw"); err != nil || p != "/bin/rbw" {
		t.Errorf("LookPath = %q, %v", p, err)
	}
}
