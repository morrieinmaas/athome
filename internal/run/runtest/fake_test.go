package runtest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/run/runtest"
)

func TestFakeRecords(t *testing.T) {
	f := runtest.NewFake()
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
	f := runtest.NewFake()
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
	f := runtest.NewFake()
	f.On("git", "checkout", "main").Return(run.Result{Code: 1, Stderr: "no"})
	res, err := f.Run(context.Background(), run.Cmd{Name: "git", Args: []string{"checkout", "main"}})
	if err != nil || res.Code != 1 || res.Stderr != "no" {
		t.Errorf("got %+v, %v", res, err)
	}
}

func TestFakeHave(t *testing.T) {
	f := runtest.NewFake()
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

func TestFakeRecordsInteractive(t *testing.T) {
	f := runtest.NewFake()
	f.Run(context.Background(), run.Cmd{Name: "x", Interactive: true})
	if c := f.Calls(); len(c) != 1 || !c[0].Interactive {
		t.Errorf("Fake must record Interactive, got %+v", c)
	}
}
