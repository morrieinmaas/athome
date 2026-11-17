package tui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/morrieinmaas/athome/internal/steps"
)

func fake(name string, done bool, apply func(context.Context, *steps.Env) error) steps.Step {
	return steps.Step{
		Name:  name,
		Check: func(context.Context, *steps.Env) (bool, error) { return done, nil },
		Apply: apply,
	}
}

// startProgress runs list under a teatest program, the way RunProgress does.
func startProgress(t *testing.T, ctx context.Context, env *steps.Env, list []steps.Step, wire func(func(func()))) (*progressRun, *teatest.TestModel) {
	t.Helper()
	r := newProgressRun(ctx, env, list)
	tm := teatest.NewTestModel(t, r.m, teatest.WithInitialTermSize(80, 24))
	r.start(tm.GetProgram(), wire)
	return r, tm
}

func waitOutput(t *testing.T, tm *teatest.TestModel, want string) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool { return bytes.Contains(b, []byte(want)) },
		teatest.WithDuration(5*time.Second), teatest.WithCheckInterval(10*time.Millisecond))
}

func TestProgressTickSkipCrossLogAndError(t *testing.T) {
	gate := make(chan struct{})
	boom := errors.New("boom")
	list := []steps.Step{
		fake("already", true, nil),
		fake("applied", false, func(ctx context.Context, env *steps.Env) error {
			env.Log.Write([]byte("hello run log\n"))
			<-gate
			return nil
		}),
		fake("broken", false, func(context.Context, *steps.Env) error { return boom }),
	}
	var orig bytes.Buffer
	env := &steps.Env{Log: &orig}
	r, tm := startProgress(t, context.Background(), env, list, nil)

	waitOutput(t, tm, "already")
	tm.Type("l")
	waitOutput(t, tm, "hello run log")
	close(gate)
	waitOutput(t, tm, "any other key: exit")
	tm.Type("x")
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))

	rep, err := r.result(nil)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if len(rep.Skipped) != 1 || len(rep.Applied) != 1 {
		t.Errorf("report = %+v", rep)
	}
	view := r.m.View()
	for _, w := range []string{"✓ applied", "- already (skipped)", "✗ broken: boom"} {
		if !strings.Contains(view, w) {
			t.Errorf("missing %q in %q", w, view)
		}
	}
	if orig.Len() != 0 {
		t.Errorf("the run log leaked to the original writer during the view: %q", orig.String())
	}
	if !strings.Contains(r.m.log.String(), "hello run log") {
		t.Errorf("in-view log missing the line: %q", r.m.log.String())
	}
	if env.Log != &orig {
		t.Error("env.Log not restored")
	}
}

func TestProgressAroundReleasesAndRestores(t *testing.T) {
	var around func(func())
	var mu sync.Mutex
	var order []string
	note := func(s string) { mu.Lock(); order = append(order, s); mu.Unlock() }
	list := []steps.Step{fake("s", false, func(context.Context, *steps.Env) error {
		around(func() { note("fn") })
		return nil
	})}
	r := newProgressRun(context.Background(), &steps.Env{}, list)
	tm := teatest.NewTestModel(t, r.m, teatest.WithInitialTermSize(80, 24))
	p := tm.GetProgram()
	r.release = func() error { note("release"); return p.ReleaseTerminal() }
	r.restore = func() error { note("restore"); return p.RestoreTerminal() }
	r.start(p, func(a func(func())) {
		if a == nil {
			t.Fatal("nil around")
		}
		around = a
	})
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
	if _, err := r.result(nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := strings.Join(order, ","); got != "release,fn,restore" {
		t.Errorf("order = %q, want release,fn,restore", got)
	}
}

// After a failure `l` toggles the log and only other keys quit.
func TestProgressFailureLKeyTogglesLog(t *testing.T) {
	boom := errors.New("boom")
	list := []steps.Step{fake("broken", false, func(_ context.Context, env *steps.Env) error {
		env.Log.Write([]byte("diagnostic line\n"))
		return boom
	})}
	r, tm := startProgress(t, context.Background(), &steps.Env{}, list, nil)
	waitOutput(t, tm, "any other key: exit")
	tm.Type("l")
	waitOutput(t, tm, "diagnostic line")
	tm.Type("x")
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
	if _, err := r.result(nil); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}

// Ctrl-C after a failure only leaves the view: the step's error comes back, not a cancel.
func TestProgressCtrlCAfterFailureReturnsStepError(t *testing.T) {
	boom := errors.New("boom")
	list := []steps.Step{fake("broken", false, func(context.Context, *steps.Env) error { return boom })}
	r, tm := startProgress(t, context.Background(), &steps.Env{}, list, nil)
	waitOutput(t, tm, "any other key: exit")
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
	_, err := r.result(nil)
	if !errors.Is(err, boom) || errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the step error and not a cancel", err)
	}
}

func TestProgressCtrlCCancels(t *testing.T) {
	list := []steps.Step{fake("slow", false, func(ctx context.Context, _ *steps.Env) error {
		<-ctx.Done()
		return ctx.Err()
	})}
	r, tm := startProgress(t, context.Background(), &steps.Env{}, list, nil)
	waitOutput(t, tm, "slow")
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
	if _, err := r.result(nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

// SIGTERM reaches bubbletea as a quit message, so the program ends before the
// done message: the run's own result must still come back, not (Report{}, nil).
func TestProgressQuitBeforeDoneReturnsExecuteResult(t *testing.T) {
	list := []steps.Step{
		fake("first", false, func(context.Context, *steps.Env) error { return nil }),
		fake("slow", false, func(ctx context.Context, _ *steps.Env) error {
			<-ctx.Done()
			return ctx.Err()
		}),
	}
	r, tm := startProgress(t, context.Background(), &steps.Env{}, list, nil)
	waitOutput(t, tm, "slow")
	tm.Send(tea.QuitMsg{})
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))

	rep, err := r.result(nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled (a quit before done is not success)", err)
	}
	if len(rep.Applied) != 1 || rep.Applied[0] != "first" {
		t.Errorf("report = %+v, want the step that did run", rep)
	}
}

// RunProgress itself: a clean run returns the report and restores env.
func TestRunProgressSuccess(t *testing.T) {
	list := []steps.Step{fake("a", false, func(context.Context, *steps.Env) error { return nil })}
	var out bytes.Buffer
	env := &steps.Env{}
	rep, err := RunProgress(context.Background(), strings.NewReader(""), &out, env, list, nil)
	if err != nil || len(rep.Applied) != 1 {
		t.Fatalf("rep = %+v, err = %v", rep, err)
	}
	if env.Log != nil || env.Events != nil {
		t.Error("env not restored")
	}
}
