package run_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/morrieinmaas/athome/internal/run"
)

func TestExecNonZero(t *testing.T) {
	res, err := run.Exec().Run(context.Background(), run.Cmd{Name: "false"})
	if err != nil {
		t.Fatalf("a non-zero exit is a Result, not an error: %v", err)
	}
	if res.Code != 1 {
		t.Errorf("Code = %d, want 1", res.Code)
	}
}

func TestExecExitCodeAndStreams(t *testing.T) {
	res, err := run.Exec().Run(context.Background(), run.Cmd{Name: "sh", Args: []string{"-c", "echo out; echo err >&2; exit 3"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != 3 || strings.TrimSpace(res.Stdout) != "out" || strings.TrimSpace(res.Stderr) != "err" {
		t.Errorf("got %+v, want code 3, stdout out, stderr err", res)
	}
}

func TestExecMissing(t *testing.T) {
	_, err := run.Exec().Run(context.Background(), run.Cmd{Name: "athome-no-such-binary-xyz"})
	if err == nil {
		t.Fatal("running an unknown binary must return an error")
	}
}

func TestExecStdinDirEnv(t *testing.T) {
	dir := t.TempDir()
	res, err := run.Exec().Run(context.Background(), run.Cmd{Name: "cat", Stdin: "héllo wörld\n"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stdout != "héllo wörld\n" {
		t.Errorf("stdin not passed through: %q", res.Stdout)
	}

	res, err = run.Exec().Run(context.Background(), run.Cmd{Name: "pwd", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(strings.TrimSpace(res.Stdout), strings.TrimPrefix(dir, "/private")) {
		t.Errorf("Dir not honoured: pwd = %q, dir = %q", res.Stdout, dir)
	}

	res, err = run.Exec().Run(context.Background(), run.Cmd{Name: "sh", Args: []string{"-c", "printf %s \"$ATHOME_RUN_TEST\""}, Env: []string{"ATHOME_RUN_TEST=yes"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stdout != "yes" {
		t.Errorf("Env not passed: %q", res.Stdout)
	}
}

func TestExecLookPath(t *testing.T) {
	r := run.Exec()
	if p, err := r.LookPath("sh"); err != nil || p == "" {
		t.Errorf("LookPath(sh) = %q, %v", p, err)
	}
	if _, err := r.LookPath("athome-no-such-binary-xyz"); err == nil {
		t.Errorf("LookPath of an unknown binary must error")
	}
}

func TestExecCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := run.Exec().Run(ctx, run.Cmd{Name: "sleep", Args: []string{"5"}})
	if err == nil && res.Code == 0 {
		t.Errorf("a cancelled context must stop the command")
	}
}

func TestExecInteractiveInheritsStreams(t *testing.T) {
	res, err := run.Exec().Run(context.Background(), run.Cmd{Name: "sh", Args: []string{"-c", "echo out; exit 4"}, Interactive: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != 4 {
		t.Errorf("Code = %d, want 4", res.Code)
	}
	if res.Stdout != "" || res.Stderr != "" {
		t.Errorf("an interactive command's output is not captured: %+v", res)
	}
}

func TestExecDeadlineNotHeldByChildOfChild(t *testing.T) {
	// The shell dies at the deadline but its background child keeps stdout open.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := run.Exec().Run(ctx, run.Cmd{Name: "sh", Args: []string{"-c", "sleep 5 & wait"}})
	if err == nil {
		t.Error("a command past its deadline must error")
	}
	if d := time.Since(start); d > 4*time.Second {
		t.Errorf("Run held for %v after the deadline", d)
	}
}
