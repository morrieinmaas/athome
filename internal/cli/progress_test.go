package cli

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
	"github.com/morrieinmaas/athome/internal/tui"
)

func TestProgressWireSuspendsAndAsks(t *testing.T) {
	_, _ = tuiSandbox(t)
	runForm = func(context.Context, io.Reader, io.Writer, tui.Seed) (map[string]string, error) {
		return map[string]string{"machineType": "personal", "includeAgents": "true"}, nil
	}
	suspended := 0
	runProgress = func(ctx context.Context, _ io.Reader, _ io.Writer, env *steps.Env, _ []steps.Step, wire func(func(func()))) (steps.Report, error) {
		wire(func(fn func()) { suspended++; fn() })
		if env.Ask == nil || env.Ask.Around == nil {
			t.Error("Ask must stay set with Around wired")
		}
		env.Run.Run(ctx, run.Cmd{Name: "bw-setup", Interactive: true})
		env.Run.Run(ctx, run.Cmd{Name: "git"})
		return steps.Report{}, nil
	}
	if code, _, e := run2("init"); code != 0 {
		t.Fatalf("exit %d: %s", code, e)
	}
	if suspended != 1 {
		t.Errorf("only the interactive command suspends: got %d", suspended)
	}
}

func TestProgressFailureAndCancel(t *testing.T) {
	_, _ = tuiSandbox(t)
	runForm = func(context.Context, io.Reader, io.Writer, tui.Seed) (map[string]string, error) {
		return map[string]string{"machineType": "personal"}, nil
	}
	runProgress = func(context.Context, io.Reader, io.Writer, *steps.Env, []steps.Step, func(func(func()))) (steps.Report, error) {
		return steps.Report{}, errors.New("boom")
	}
	code, _, stderr := run2("init")
	if code != 1 || !strings.Contains(stderr, "athome init: boom") {
		t.Errorf("failure: exit %d stderr %q", code, stderr)
	}
	runProgress = func(context.Context, io.Reader, io.Writer, *steps.Env, []steps.Step, func(func(func()))) (steps.Report, error) {
		return steps.Report{}, errors.Join(errors.New("run canceled"), context.Canceled)
	}
	if code, _, _ := run2("init"); code != 130 {
		t.Errorf("cancel: exit %d, want 130", code)
	}
}

// Ctrl-C in the progress view still runs the registered cleanups, and exits 130.
func TestProgressCancelRunsCleanup(t *testing.T) {
	_, _ = tuiSandbox(t)
	runForm = func(context.Context, io.Reader, io.Writer, tui.Seed) (map[string]string, error) {
		return map[string]string{"machineType": "personal"}, nil
	}
	cleaned := false
	runProgress = func(_ context.Context, _ io.Reader, _ io.Writer, env *steps.Env, _ []steps.Step, _ func(func(func()))) (steps.Report, error) {
		env.Cleanup = append(env.Cleanup, func() { cleaned = true })
		return steps.Report{}, errors.Join(errors.New("run canceled"), context.Canceled)
	}
	if code, _, _ := run2("init"); code != 130 {
		t.Errorf("exit %d, want 130", code)
	}
	if !cleaned {
		t.Error("env.Cleanup did not run after a cancel")
	}
}
