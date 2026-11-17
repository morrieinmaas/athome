package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/morrieinmaas/athome/internal/prompt"
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

	if orig.Len() != 0 {
		t.Errorf("the run log leaked to the original writer during the view: %q", orig.String())
	}
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
	if !strings.Contains(orig.String(), "hello run log") {
		t.Errorf("the run log is not written to the original writer after the view: %q", orig.String())
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

// A step that runs an interactive command after the view quit must not release
// or restore the dead program: the command just runs.
func TestProgressAroundAfterQuitSkipsTerminalHandOver(t *testing.T) {
	var around func(func())
	var calls, ran atomic.Int32
	list := []steps.Step{fake("slow", false, func(ctx context.Context, _ *steps.Env) error {
		<-ctx.Done() // result() cancels after the program is gone
		around(func() { ran.Add(1) })
		return ctx.Err()
	})}
	r := newProgressRun(context.Background(), &steps.Env{}, list)
	tm := teatest.NewTestModel(t, r.m, teatest.WithInitialTermSize(80, 24))
	r.release = func() error { calls.Add(1); return nil }
	r.restore = func() error { calls.Add(1); return nil }
	r.start(tm.GetProgram(), func(a func(func())) { around = a })
	waitOutput(t, tm, "slow")
	tm.Send(tea.QuitMsg{})
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
	_, _ = r.result(nil)
	if ran.Load() != 1 {
		t.Errorf("fn ran %d times, want 1", ran.Load())
	}
	if calls.Load() != 0 {
		t.Errorf("release/restore called %d times after the view quit", calls.Load())
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

// An external SIGINT makes p.Run return tea.ErrInterrupted; that is a cancel, not a failure.
func TestProgressInterruptedIsCancel(t *testing.T) {
	list := []steps.Step{fake("a", false, func(context.Context, *steps.Env) error { return nil })}
	r := newProgressRun(context.Background(), &steps.Env{}, list)
	tm := teatest.NewTestModel(t, r.m, teatest.WithInitialTermSize(80, 24))
	r.start(tm.GetProgram(), nil)
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
	if _, err := r.result(tea.ErrInterrupted); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want one wrapping context.Canceled", err)
	}
}

// The log of a failed run is printed after the view is gone.
func TestRunProgressWritesLogAfterExit(t *testing.T) {
	list := []steps.Step{fake("a", false, func(_ context.Context, env *steps.Env) error {
		env.Log.Write([]byte("==> manual upload instructions\n"))
		return nil
	})}
	var orig bytes.Buffer
	env := &steps.Env{Log: &orig}
	if _, err := RunProgress(context.Background(), strings.NewReader(""), &bytes.Buffer{}, env, list, nil); err != nil {
		t.Fatal(err)
	}
	if got := orig.String(); !strings.Contains(got, "==> manual upload instructions\n") {
		t.Errorf("log after the view = %q", got)
	}
}

// The view at 40 columns still shows every step and its hint, within the width.
func TestProgressAtWidth40(t *testing.T) {
	gate := make(chan struct{})
	list := []steps.Step{
		fake("first", false, func(context.Context, *steps.Env) error { return nil }),
		fake("second", false, func(ctx context.Context, _ *steps.Env) error {
			select {
			case <-gate:
			case <-ctx.Done():
			}
			return nil
		}),
	}
	r := newProgressRun(context.Background(), &steps.Env{}, list)
	tm := teatest.NewTestModel(t, r.m, teatest.WithInitialTermSize(40, 24))
	r.start(tm.GetProgram(), nil)
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("ctrl-c: cancel")) && bytes.Contains(b, []byte("first"))
	}, teatest.WithDuration(5*time.Second), teatest.WithCheckInterval(10*time.Millisecond))
	for _, line := range strings.Split(r.m.View(), "\n") {
		if w := lipgloss.Width(line); w > 40 {
			t.Errorf("line is %d wide: %q", w, line)
		}
	}
	close(gate)
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
	if _, err := r.result(nil); err != nil {
		t.Fatal(err)
	}
}

// Ctrl-C while the terminal is released cancels the parent context: the view
// quits at once with a cancel, not a failure that waits for a key.
func TestProgressParentCancelQuitsAsCancel(t *testing.T) {
	for name, stepErr := range map[string]func(context.Context) error{
		"context error": func(ctx context.Context) error { return ctx.Err() },
		"other error":   func(context.Context) error { return errors.New("signal: interrupt") },
	} {
		t.Run(name, func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			list := []steps.Step{fake("slow", false, func(ctx context.Context, _ *steps.Env) error {
				<-ctx.Done()
				return stepErr(ctx)
			})}
			r, tm := startProgress(t, parent, &steps.Env{}, list, nil)
			waitOutput(t, tm, "slow")
			cancel()
			tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
			if _, err := r.result(nil); !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want a cancel", err)
			}
		})
	}
}

// With more steps than rows, bubbletea would drop the top of the view: the view
// must fit the terminal, keep the running step and, with the log on, its last line.
func TestProgressViewFitsTerminalHeight(t *testing.T) {
	var list []steps.Step
	for i := 0; i < 32; i++ {
		list = append(list, fake(fmt.Sprintf("step-%02d", i), true, nil))
	}
	r := newProgressRun(context.Background(), &steps.Env{}, list)
	defer r.cancel()
	m := r.m
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	for i := range m.rows {
		switch {
		case i < 20:
			m.rows[i].state = "done"
		case i == 20:
			m.rows[i].state = "apply"
		}
	}
	for i := 0; i < 40; i++ {
		fmt.Fprintf(m.log, "log line %02d\n", i)
	}
	check := func(name string, wantLog bool) {
		t.Helper()
		v := m.View()
		if n := strings.Count(v, "\n") + 1; n > 24 {
			t.Errorf("%s: view has %d lines, the terminal has 24:\n%s", name, n, v)
		}
		if !strings.Contains(v, "step-20") {
			t.Errorf("%s: the running step is not visible:\n%s", name, v)
		}
		if got := strings.Contains(v, "log line 39"); got != wantLog {
			t.Errorf("%s: last log line visible = %v, want %v:\n%s", name, got, wantLog, v)
		}
	}
	check("log off", false)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	check("log on", true)

	m.rows[20].state = "done"
	for i := 21; i < len(m.rows); i++ {
		m.rows[i].state = "done"
	}
	if v := m.View(); !strings.Contains(v, "step-31") {
		t.Errorf("the last step is not visible when all are done:\n%s", v)
	}
}

// A declined update confirm ends the view at once, without a key press.
func TestProgressDeclinedUpdateQuitsWithoutKey(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	var out bytes.Buffer
	env := &steps.Env{Ask: prompt.New(strings.NewReader("n\n"), &out)}
	list := []steps.Step{fake("self", false, func(_ context.Context, env *steps.Env) error {
		if env.Ask.Confirm("update?", true) {
			return nil
		}
		return steps.ErrUpdateCancelled
	})}
	done := make(chan error, 1)
	go func() {
		_, err := RunProgress(context.Background(), pr, io.Discard, env, list, nil)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, steps.ErrUpdateCancelled) {
			t.Errorf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the view waited for a key after a declined update")
	}
}
