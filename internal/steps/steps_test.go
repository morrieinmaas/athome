package steps_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/config"
	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
)

// ---- shared helpers for every *_test.go in this package ----------------------
//
// Step names other than "repo-check" (first) and "checkout-main" (last) are not
// fixed by the plan, so group tests run a whole group (Prepare(), SSHKey(), ...)
// through steps.Execute and assert on the commands the Fake recorded and on the
// files left in the temp home.

const testHost = "testhost"

// fixture is one temp home plus a minimal athome checkout and a Fake runner.
type fixture struct {
	T    *testing.T
	Home string
	Repo string
	Fake *run.Fake
	Log  *bytes.Buffer
	Env  *steps.Env
}

// newFixture builds an Env whose home and repo live under t.TempDir(). The repo
// has .chezmoiroot, home/.chezmoiscripts/, a .git dir and the hook sources. The
// chezmoi hostname query is stubbed to testHost, the first branch query to main.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home dir") // a space on purpose
	repo := filepath.Join(root, "src repo")
	for _, d := range []string{
		home,
		filepath.Join(repo, "home", ".chezmoiscripts"),
		filepath.Join(repo, "home", "dot_config", "git", "hooks"),
		filepath.Join(repo, ".git"),
	} {
		mustMkdir(t, d)
	}
	writeFile(t, filepath.Join(repo, ".chezmoiroot"), "home\n", 0o644)
	writeFile(t, filepath.Join(repo, "home", "dot_config", "git", "hooks", "executable_pre-commit"), "#!/bin/sh\n# pre-commit\n", 0o644)
	writeFile(t, filepath.Join(repo, "home", "dot_config", "git", "hooks", "executable_commit-msg"), "#!/bin/sh\n# commit-msg\n", 0o644)

	f := run.NewFake()
	f.On("chezmoi", "execute-template", "{{ .chezmoi.hostname }}").Return(run.Result{Stdout: testHost + "\n"})
	f.On("git", "-C", repo, "rev-parse", "--abbrev-ref", "HEAD").Return(run.Result{Stdout: "main\n"})

	log := &bytes.Buffer{}
	return &fixture{
		T: t, Home: home, Repo: repo, Fake: f, Log: log,
		Env: &steps.Env{
			Cfg:      config.Config{Machine: "personal", IncludeAgents: true, Data: map[string]string{}},
			Run:      f,
			Home:     home,
			RepoRoot: repo,
			OS:       "linux",
			Distro:   "arch",
			Log:      log,
		},
	}
}

// Exec runs a step list against the fixture.
func (fx *fixture) Exec(list []steps.Step) (steps.Report, error) {
	fx.T.Helper()
	return steps.Execute(context.Background(), fx.Env, list)
}

func (fx *fixture) MustExec(list []steps.Step) steps.Report {
	fx.T.Helper()
	rep, err := fx.Exec(list)
	if err != nil {
		fx.T.Fatalf("Execute: %v\nlog:\n%s", err, fx.Log)
	}
	return rep
}

func (fx *fixture) Path(parts ...string) string {
	return filepath.Join(append([]string{fx.Home}, parts...)...)
}

// PutKey writes ~/.ssh/<testHost>_ed25519 (and its .pub when pub != "").
func (fx *fixture) PutKey(priv, pub string) string {
	fx.T.Helper()
	key := fx.Path(".ssh", testHost+"_ed25519")
	mustMkdir(fx.T, filepath.Dir(key))
	writeFile(fx.T, key, priv, 0o600)
	if pub != "" {
		writeFile(fx.T, key+".pub", pub, 0o644)
	}
	return key
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// containsSeq reports whether want appears as a contiguous run inside args.
func containsSeq(args []string, want ...string) bool {
	if len(want) == 0 {
		return true
	}
	for i := 0; i+len(want) <= len(args); i++ {
		if reflect.DeepEqual(args[i:i+len(want)], want) {
			return true
		}
	}
	return false
}

// callsTo returns the recorded calls to a command whose arguments contain the
// given contiguous run (git's `-C <dir>` prefix is therefore irrelevant).
func (fx *fixture) callsTo(name string, argSeq ...string) []run.Cmd {
	var out []run.Cmd
	for _, c := range fx.Fake.Calls() {
		if c.Name == name && containsSeq(c.Args, argSeq...) {
			out = append(out, c)
		}
	}
	return out
}

func (fx *fixture) called(name string, argSeq ...string) bool {
	return len(fx.callsTo(name, argSeq...)) > 0
}

// mentions reports whether any recorded call has needle in its name, args or stdin.
func (fx *fixture) mentions(needle string) bool {
	for _, c := range fx.Fake.Calls() {
		if strings.Contains(c.Name, needle) || strings.Contains(strings.Join(c.Args, " "), needle) || strings.Contains(c.Stdin, needle) {
			return true
		}
	}
	return false
}

func (fx *fixture) dump() string {
	var b strings.Builder
	for _, c := range fx.Fake.Calls() {
		b.WriteString(c.Name + " " + strings.Join(c.Args, " ") + "\n")
	}
	return b.String()
}

// ---- steps core (T3) -------------------------------------------------------

type recorder struct {
	checked, applied []string
}

// mk builds a step whose check consults done[name] and whose apply flips it.
func mk(r *recorder, done map[string]bool, name string, applyErr error) steps.Step {
	return steps.Step{
		Name: name,
		Check: func(context.Context, *steps.Env) (bool, error) {
			r.checked = append(r.checked, name)
			return done[name], nil
		},
		Apply: func(context.Context, *steps.Env) error {
			r.applied = append(r.applied, name)
			if applyErr != nil {
				return applyErr
			}
			done[name] = true
			return nil
		},
	}
}

func coreEnv() *steps.Env {
	return &steps.Env{Run: run.NewFake(), Log: &bytes.Buffer{}}
}

func TestSkipDone(t *testing.T) {
	r := &recorder{}
	done := map[string]bool{"a": true}
	rep, err := steps.Execute(context.Background(), coreEnv(), []steps.Step{mk(r, done, "a", nil), mk(r, done, "b", nil)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rep.Skipped, []string{"a"}) || !reflect.DeepEqual(rep.Applied, []string{"b"}) {
		t.Errorf("report = %+v, want Skipped [a] Applied [b]", rep)
	}
	if !reflect.DeepEqual(r.applied, []string{"b"}) {
		t.Errorf("apply ran for %v, want only b", r.applied)
	}
}

func TestExecuteEmptyList(t *testing.T) {
	rep, err := steps.Execute(context.Background(), coreEnv(), nil)
	if err != nil || len(rep.Applied) != 0 || len(rep.Skipped) != 0 {
		t.Errorf("got %+v, %v; want an empty report", rep, err)
	}
}

func TestExecuteKeepsOrder(t *testing.T) {
	r := &recorder{}
	done := map[string]bool{}
	rep, err := steps.Execute(context.Background(), coreEnv(), []steps.Step{mk(r, done, "x", nil), mk(r, done, "y", nil), mk(r, done, "z", nil)})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rep.Applied, []string{"x", "y", "z"}) || !reflect.DeepEqual(r.applied, []string{"x", "y", "z"}) {
		t.Errorf("applied %v / report %v, want x y z in order", r.applied, rep.Applied)
	}
}

func TestStopOnError(t *testing.T) {
	errX := errors.New("boom-x")
	r := &recorder{}
	done := map[string]bool{}
	rep, err := steps.Execute(context.Background(), coreEnv(), []steps.Step{
		mk(r, done, "a", nil), mk(r, done, "b", errX), mk(r, done, "c", nil),
	})
	if err == nil {
		t.Fatal("want an error")
	}
	if !errors.Is(err, errX) {
		t.Errorf("error %v does not wrap errX", err)
	}
	if !strings.Contains(err.Error(), "b") {
		t.Errorf("error %q does not name step b", err)
	}
	if !reflect.DeepEqual(rep.Applied, []string{"a"}) {
		t.Errorf("Applied = %v, want [a] (the failed step and later ones excluded)", rep.Applied)
	}
	if reflect.DeepEqual(r.applied, []string{"a", "b", "c"}) || len(r.applied) > 2 {
		t.Errorf("step c must not run after b failed: applied %v", r.applied)
	}
}

func TestCheckErrorStopsAndNamesStep(t *testing.T) {
	errCheck := errors.New("cannot tell")
	applied := false
	list := []steps.Step{{
		Name:  "probe",
		Check: func(context.Context, *steps.Env) (bool, error) { return false, errCheck },
		Apply: func(context.Context, *steps.Env) error { applied = true; return nil },
	}}
	_, err := steps.Execute(context.Background(), coreEnv(), list)
	if !errors.Is(err, errCheck) || !strings.Contains(err.Error(), "probe") {
		t.Errorf("error = %v, want one naming probe and wrapping the check error", err)
	}
	if applied {
		t.Errorf("apply must not run when the check errored")
	}
}

func TestResume(t *testing.T) {
	errX := errors.New("transient")
	done := map[string]bool{}

	r1 := &recorder{}
	_, err := steps.Execute(context.Background(), coreEnv(), []steps.Step{
		mk(r1, done, "a", nil), mk(r1, done, "b", errX), mk(r1, done, "c", nil),
	})
	if err == nil {
		t.Fatal("first run must fail")
	}

	r2 := &recorder{}
	rep, err := steps.Execute(context.Background(), coreEnv(), []steps.Step{
		mk(r2, done, "a", nil), mk(r2, done, "b", nil), mk(r2, done, "c", nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rep.Skipped, []string{"a"}) || !reflect.DeepEqual(rep.Applied, []string{"b", "c"}) {
		t.Errorf("rerun report = %+v, want Skipped [a] Applied [b c]", rep)
	}
	if !reflect.DeepEqual(r2.applied, []string{"b", "c"}) {
		t.Errorf("rerun applied %v, want [b c]", r2.applied)
	}
}

func TestIdempotent(t *testing.T) {
	done := map[string]bool{}
	list := func(r *recorder) []steps.Step {
		return []steps.Step{mk(r, done, "a", nil), mk(r, done, "b", nil), mk(r, done, "c", nil)}
	}
	r1 := &recorder{}
	if _, err := steps.Execute(context.Background(), coreEnv(), list(r1)); err != nil {
		t.Fatal(err)
	}
	r2 := &recorder{}
	rep, err := steps.Execute(context.Background(), coreEnv(), list(r2))
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Applied) != 0 || len(r2.applied) != 0 {
		t.Errorf("second run applied %v / %v, want nothing", rep.Applied, r2.applied)
	}
	if !reflect.DeepEqual(rep.Skipped, []string{"a", "b", "c"}) {
		t.Errorf("second run skipped %v, want a b c", rep.Skipped)
	}
}
