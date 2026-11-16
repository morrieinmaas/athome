package cli_test

import (
	"context"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/cli"
	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/run/runtest"
	"github.com/morrieinmaas/athome/internal/steps"
)

// R8: the config file in use is logged (B:188), also when its answers are then rejected.
func TestConfigFileIsLogged(t *testing.T) {
	sandbox(t)
	inRepo(t)
	cfg := tmpFile(t, "a.toml", "machine = \"laptop\"\n")
	code, out, _ := runMain("init", "--config", cfg)
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if want := "==> non-interactive config: " + cfg + "\n"; !strings.Contains(out, want) {
		t.Errorf("stdout %q lacks %q", out, want)
	}
}

func TestNoConfigNoLogLine(t *testing.T) {
	sandbox(t)
	inRepo(t)
	_, out, _ := runMain("init", "--machine", "laptop")
	if strings.Contains(out, "non-interactive config") {
		t.Errorf("no config file, no log line, got %q", out)
	}
}

// U13: sudo is requested only when a step first runs a sudo command, and it
// is not a step.
func TestSudoStartsWhenAStepNeedsIt(t *testing.T) {
	f := runtest.NewFake()
	env := &steps.Env{Run: f, OS: "linux"}
	list := []steps.Step{
		{
			Name:  "quiet",
			Check: func(context.Context, *steps.Env) (bool, error) { return false, nil },
			Apply: func(ctx context.Context, e *steps.Env) error {
				if n := len(f.Calls()); n != 0 {
					t.Errorf("a step without sudo must not trigger sudo -v: %v", f.Calls())
				}
				return nil
			},
		},
		{
			Name:  "needs-sudo",
			Check: func(context.Context, *steps.Env) (bool, error) { return false, nil },
			Apply: func(ctx context.Context, e *steps.Env) error {
				e.Run.Run(ctx, run.Cmd{Name: "sudo", Args: []string{"true"}})
				e.Run.Run(ctx, run.Cmd{Name: "sudo", Args: []string{"true"}})
				return nil
			},
		},
	}
	rep, err := cli.Execute(context.Background(), env, list)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range f.Calls() {
		names = append(names, c.Name+" "+strings.Join(c.Args, " "))
	}
	want := []string{"sudo -v", "sudo true", "sudo true"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("calls = %q, want %q", names, want)
	}
	if len(rep.Applied) != 2 {
		t.Errorf("sudo must not add a step: %+v", rep)
	}
	if env.Run != run.Runner(f) {
		t.Errorf("Execute must leave env.Run as it found it")
	}
}

func TestNoSudoWhenNoStepNeedsIt(t *testing.T) {
	f := runtest.NewFake()
	env := &steps.Env{Run: f, OS: "linux"}
	list := []steps.Step{{
		Name:  "plain",
		Check: func(context.Context, *steps.Env) (bool, error) { return true, nil },
		Apply: func(context.Context, *steps.Env) error { return nil },
	}}
	if _, err := cli.Execute(context.Background(), env, list); err != nil {
		t.Fatal(err)
	}
	if n := len(f.Calls()); n != 0 {
		t.Errorf("no step ran sudo, but the runner saw %v", f.Calls())
	}
}

// Safety net: init with valid args in a repo fixture goes through the Fake, never
// a binary on PATH. PATH is an empty directory (see TestMain).
func TestInitWithValidArgsOnlyTouchesTheFake(t *testing.T) {
	sandbox(t)
	inRepo(t)
	if cli.RealRunnerInstalled() {
		t.Fatal("the real runner is installed in a test")
	}
	if p, err := exec.LookPath("sh"); err == nil {
		t.Fatalf("PATH reaches real binaries (%s); TestMain must empty it", p)
	}
	f := runtest.NewFake()
	defer cli.SetRunner(f)()
	cfg := tmpFile(t, "a.toml", "machine = \"personal\"\nincludeAgents = false\n")
	code, _, _ := runMain("init", "--config", cfg, "--non-interactive")
	if len(f.Calls()) == 0 {
		t.Errorf("exit %d: the run never reached the runner; the fixture is not a valid init", code)
	}
}

// Y2: a follow-up log that cannot be emptied stops the run with exit 1.
func TestInitFailsWhenFollowupLogCannotBeEmptied(t *testing.T) {
	sandbox(t)
	inRepo(t)
	if err := os.Mkdir(steps.FollowupLogPath(), 0o755); err != nil { // a directory where the log should be
		t.Fatal(err)
	}
	defer cli.SetRunner(runtest.NewFake())()
	cfg := tmpFile(t, "a.toml", "machine = \"personal\"\nincludeAgents = false\n")
	code, _, stderr := runMain("init", "--config", cfg, "--non-interactive")
	if code != 1 || !strings.Contains(stderr, "follow-up") {
		t.Errorf("exit %d, stderr %q; want exit 1 naming the follow-up log", code, stderr)
	}
}
