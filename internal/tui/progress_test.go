package tui_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/morrieinmaas/athome/internal/steps"
	"github.com/morrieinmaas/athome/internal/tui"
)

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func waitFor(t *testing.T, out *syncBuf, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q in %q", want, out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func fake(name string, done bool, apply func(context.Context, *steps.Env) error) steps.Step {
	return steps.Step{
		Name:  name,
		Check: func(context.Context, *steps.Env) (bool, error) { return done, nil },
		Apply: apply,
	}
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
	pr, pw := io.Pipe()
	var out syncBuf
	var orig bytes.Buffer
	env := &steps.Env{Log: &orig}
	type result struct {
		rep steps.Report
		err error
	}
	res := make(chan result, 1)
	go func() {
		rep, err := tui.RunProgress(context.Background(), pr, &out, env, list, nil)
		res <- result{rep, err}
	}()

	waitFor(t, &out, "already")
	pw.Write([]byte("l"))
	waitFor(t, &out, "hello run log")
	close(gate)
	waitFor(t, &out, "✗")
	pw.Write([]byte("x"))

	r := <-res
	if !errors.Is(r.err, boom) {
		t.Fatalf("err = %v", r.err)
	}
	for _, w := range []string{"✓", "skipped", "✗"} {
		if !strings.Contains(out.String(), w) {
			t.Errorf("missing %q", w)
		}
	}
	if !strings.Contains(orig.String(), "hello run log") {
		t.Errorf("original log not forwarded: %q", orig.String())
	}
	if env.Log != &orig {
		t.Error("env.Log not restored")
	}
}

func TestProgressAroundReleasesAndRestores(t *testing.T) {
	var around func(func())
	ran := false
	list := []steps.Step{fake("s", false, func(context.Context, *steps.Env) error {
		around(func() { ran = true })
		return nil
	})}
	env := &steps.Env{}
	var out syncBuf
	_, err := tui.RunProgress(context.Background(), strings.NewReader(""), &out, env, list, func(a func(func())) {
		if a == nil {
			t.Fatal("nil around")
		}
		around = a
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Error("around did not run fn")
	}
}

func TestProgressCtrlCCancels(t *testing.T) {
	list := []steps.Step{fake("slow", false, func(ctx context.Context, _ *steps.Env) error {
		<-ctx.Done()
		return ctx.Err()
	})}
	pr, pw := io.Pipe()
	var out syncBuf
	res := make(chan error, 1)
	go func() {
		_, err := tui.RunProgress(context.Background(), pr, &out, &steps.Env{}, list, nil)
		res <- err
	}()
	waitFor(t, &out, "slow")
	pw.Write([]byte{3})
	if err := <-res; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
