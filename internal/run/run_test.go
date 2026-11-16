package run_test

import (
	"context"
	"strings"
	"testing"

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

func TestFakeRecords(t *testing.T) {
	f := run.NewFake()
	ctx := context.Background()
	f.Run(ctx, run.Cmd{Name: "a", Args: []string{"1"}})
	f.Run(ctx, run.Cmd{Name: "b", Args: []string{"2", "3"}, Dir: "/x", Stdin: "in", Env: []string{"K=V"}})
	f.Run(ctx, run.Cmd{Name: "c"})
	calls := f.Calls()
	if len(calls) != 3 {
		t.Fatalf("len(Calls) = %d, want 3", len(calls))
	}
	for i, want := range []string{"a", "b", "c"} {
		if calls[i].Name != want {
			t.Errorf("call %d = %q, want %q", i, calls[i].Name, want)
		}
	}
	if calls[1].Dir != "/x" || calls[1].Stdin != "in" || calls[1].Env[0] != "K=V" || strings.Join(calls[1].Args, ",") != "2,3" {
		t.Errorf("call 1 lost fields: %+v", calls[1])
	}
}

func TestFakeReturn(t *testing.T) {
	f := run.NewFake()
	f.On("gh", "api", "user").Return(run.Result{Stdout: `{"login":"a"}`})
	ctx := context.Background()

	res, err := f.Run(ctx, run.Cmd{Name: "gh", Args: []string{"api", "user"}})
	if err != nil || res.Stdout != `{"login":"a"}` {
		t.Errorf("matched call = %+v, %v", res, err)
	}

	res, err = f.Run(ctx, run.Cmd{Name: "gh", Args: []string{"api", "other"}})
	if err != nil || res.Code != 0 || res.Stdout != "" {
		t.Errorf("unmatched call = %+v, %v; want zero Result, nil", res, err)
	}
	if len(f.Calls()) != 2 {
		t.Errorf("unmatched call must still be recorded, got %d calls", len(f.Calls()))
	}
}

func TestFakeReturnsNonZero(t *testing.T) {
	f := run.NewFake()
	f.On("git", "checkout", "main").Return(run.Result{Code: 1, Stderr: "no"})
	res, err := f.Run(context.Background(), run.Cmd{Name: "git", Args: []string{"checkout", "main"}})
	if err != nil || res.Code != 1 || res.Stderr != "no" {
		t.Errorf("got %+v, %v", res, err)
	}
}

func TestFakeHave(t *testing.T) {
	f := run.NewFake()
	f.Have("gh", "rbw")
	for _, n := range []string{"gh", "rbw"} {
		if _, err := f.LookPath(n); err != nil {
			t.Errorf("LookPath(%s) after Have: %v", n, err)
		}
	}
	if _, err := f.LookPath("chezmoi"); err == nil {
		t.Errorf("LookPath of a tool not declared with Have must error")
	}
}
