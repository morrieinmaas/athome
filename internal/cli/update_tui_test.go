package cli

import (
	"context"
	"io"
	"reflect"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/run/runtest"
	"github.com/morrieinmaas/athome/internal/steps"
	"github.com/morrieinmaas/athome/internal/tui"
)

// updateSandbox is tuiSandbox with a fake runner that knows one release tag.
func updateSandbox(t *testing.T, version string) (shown *[]string, f *runtest.Fake, repo string) {
	t.Helper()
	home, shown := tuiSandbox(t)
	repo, err := findRepoRoot(home)
	if err != nil {
		t.Fatal(err)
	}
	f = runtest.NewFake()
	f.On("git", "-C", repo, "tag", "--list").Return(run.Result{Stdout: "v1.1.0\n"})
	f.On("mise", "ls", "--missing", "--json").Return(run.Result{Stdout: "{}"})
	newRunner = func() run.Runner { return f }
	old, oldExec := Version, updateExec
	Version = version
	updateExec = func(string, []string) error { return nil }
	t.Cleanup(func() { Version, updateExec = old, oldExec })
	return shown, f, repo
}

func TestUpdateRoutesByTerminalAndFlags(t *testing.T) {
	for _, c := range []struct {
		name    string
		args    []string
		in, out bool
		want    []string
	}{
		{"terminal", []string{"update"}, true, true, []string{"progress"}},
		{"yes", []string{"update", "--yes"}, true, true, nil},
		{"y", []string{"update", "-y"}, true, true, nil},
		{"stdin pipe", []string{"update"}, false, true, nil},
		{"stdout pipe", []string{"update"}, true, false, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			shown, f, _ := updateSandbox(t, "dev")
			stdinIsTTY, stdoutIsTTY = func() bool { return c.in }, func() bool { return c.out }
			if code, _, stderr := run2(c.args...); code != 0 {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			if !reflect.DeepEqual(*shown, c.want) && !(len(*shown) == 0 && len(c.want) == 0) {
				t.Errorf("screens = %v, want %v", *shown, c.want)
			}
			if !hasCall(f, "mise", "ls", "--missing", "--json") {
				t.Errorf("the update steps did not run: %v", f.Calls())
			}
		})
	}
}

func hasCall(f *runtest.Fake, name string, args ...string) bool {
	for _, c := range f.Calls() {
		if c.Name == name && reflect.DeepEqual(c.Args, args) {
			return true
		}
	}
	return false
}

// On the progress view the update's interactive commands go through the
// suspending runner, and the changelog and question reach the released terminal.
func TestUpdateProgressIsWiredLikeInit(t *testing.T) {
	_, f, _ := updateSandbox(t, "1.0.0")
	var released, wired bool
	runProgress = func(ctx context.Context, _ io.Reader, _ io.Writer, env *steps.Env, list []steps.Step, wire func(func(func()))) (steps.Report, error) {
		if env.Ask == nil {
			t.Error("a terminal run must keep its confirm prompt")
		} else {
			env.Ask.Around = nil
		}
		wire(func(fn func()) { released = true; fn() })
		wired = env.Ask != nil && env.Ask.Around != nil
		env.Run.Run(ctx, run.Cmd{Name: "chezmoi", Args: []string{"apply"}, Interactive: true})
		return steps.Report{}, steps.ErrUpdateCancelled
	}
	if code, _, stderr := run2("update"); code != 0 {
		t.Fatalf("a declined update exits 0, got %d: %s", code, stderr)
	}
	if !wired {
		t.Error("env.Ask.Around is not wired")
	}
	if !released {
		t.Error("an interactive command must run around the progress view")
	}
	if !hasCall(f, "chezmoi", "apply") {
		t.Errorf("calls %v", f.Calls())
	}
}

func TestUpdateProgressInterruptExits130(t *testing.T) {
	updateSandbox(t, "dev")
	runProgress = func(context.Context, io.Reader, io.Writer, *steps.Env, []steps.Step, func(func(func()))) (steps.Report, error) {
		return steps.Report{}, context.Canceled
	}
	if code, _, _ := run2("update"); code != exitInterrupted {
		t.Errorf("exit %d, want %d", code, exitInterrupted)
	}
}

func TestHomeUpdateUsesProgressView(t *testing.T) {
	shown, f, _ := updateSandbox(t, "dev")
	runHome = func(io.Reader, io.Writer, tui.HomeInfo) (string, error) { return "update", nil }
	if code, _, stderr := run2(); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !reflect.DeepEqual(*shown, []string{"progress"}) || !hasCall(f, "mise", "ls", "--missing", "--json") {
		t.Errorf("screens %v calls %v", *shown, f.Calls())
	}
}
