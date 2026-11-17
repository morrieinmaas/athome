package steps_test

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/prompt"
	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
)

func TestOrder(t *testing.T) {
	all := steps.All()
	if len(all) == 0 {
		t.Fatal("All() is empty")
	}

	seen := map[string]bool{}
	for i, s := range all {
		if s.Name == "" {
			t.Errorf("step %d has no name", i)
		}
		if seen[s.Name] {
			t.Errorf("duplicate step name %q", s.Name)
		}
		seen[s.Name] = true
		if s.Check == nil || s.Apply == nil {
			t.Errorf("step %q needs both a Check and an Apply", s.Name)
		}
	}

	if all[0].Name != "repo-check" {
		t.Errorf("first step = %q, want repo-check", all[0].Name)
	}
	if last := all[len(all)-1].Name; last != "checkout-main" {
		t.Errorf("last step = %q, want checkout-main", last)
	}
}

// bash order: prepare, ssh key, github, data, baseline, finish.
func TestOrderIsTheGroupsInBashOrder(t *testing.T) {
	var want []steps.Step
	for _, g := range [][]steps.Step{steps.Prepare(), steps.SSHKey(), steps.GitHub(), steps.Data(), steps.Baseline(), steps.Finish()} {
		want = append(want, g...)
	}
	if got := names(steps.All()); !reflect.DeepEqual(got, names(want)) {
		t.Errorf("All() names =\n%v\nwant Prepare+SSHKey+GitHub+Data+Baseline+Finish =\n%v", got, names(want))
	}
}

func TestEveryGroupIsNonEmpty(t *testing.T) {
	groups := map[string][]steps.Step{
		"Prepare": steps.Prepare(), "SSHKey": steps.SSHKey(), "GitHub": steps.GitHub(),
		"Data": steps.Data(), "Baseline": steps.Baseline(), "Finish": steps.Finish(),
	}
	for name, g := range groups {
		if len(g) == 0 {
			t.Errorf("%s() returned no steps", name)
		}
	}
}

// Criterion 7: a second run against a converged machine applies nothing.
func TestAllTwiceAppliesNothing(t *testing.T) {
	fx := newFixture(t)
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("PATH", "/usr/bin")
	t.Cleanup(steps.SetNanobrewBin(fx.Path("absent-nb")))
	fx.Fake.Have("chezmoi", "gh", "rbw", "gitleaks", "mise", "yay")
	fx.Fake.On("sudo", "-n", "true").Return(run.Result{Code: 0})
	fx.Fake.On("chezmoi", chezmoiVerify(fx)...).Return(run.Result{Code: 0})
	fx.Env.Cfg = e2eConfig(t, fx.Home)
	fx.Env.Opts.CI = true // GitHub is never touched, so its steps are skipped
	fx.PutKey("PRIVATE\n", "ssh-ed25519 AAAA testhost\n")
	for _, d := range []string{"personal", "sidebiz"} {
		writeFile(t, fx.Path(d, ".envrc"), "x", 0o644)
	}
	// Secrets already restored; without them an unlocked rbw
	// and no secrets would restore on every run.
	writeFile(t, fx.Path(".secrets", "personal", ".env"), "x", 0o600)
	if _, err := fx.Exec(steps.All()); err != nil {
		t.Fatalf("first run: %v\n%s", err, fx.Log)
	}
	rep, err := fx.Rerun(steps.All())
	if err != nil {
		t.Fatalf("second run: %v\n%s", err, fx.Log)
	}
	if len(rep.Applied) != 0 {
		t.Errorf("second run applied %v, want nothing\n%s", rep.Applied, fx.Log)
	}
}

// Checks only read: running every step's Check, on a machine set up to reach
// the branches that used to write, leaves the Env, PATH and the follow-up log
// as they were. The writes belong to Setup and the applies.
func TestChecksArePure(t *testing.T) {
	fx := newFixture(t)
	t.Setenv("PATH", "/usr/bin")
	t.Cleanup(steps.SetNanobrewBin(fx.Path("absent-nb")))
	fx.Env.OS = "darwin"
	fx.Fake.Have("rbw", "gh") // rbw unlocked, no secrets restored
	fx.Env.Cfg = e2eConfig(t, fx.Home)
	fx.Env.Cfg.Data["sshKeyAction"] = "reupload"
	fx.PutKey("PRIVATE\n", "ssh-ed25519 AAAA testhost\n")
	writeFile(t, fx.Path(".local", "bin", "chezmoi"), "#!/bin/sh\n", 0o755)
	writeFile(t, steps.FollowupLogPath(), "  • earlier note\n", 0o644)
	// A restore marker next to a ~/.secrets/*/.env is what secrets-restore's check must not tidy.
	writeFile(t, fx.Path(".local", "state", "athome", "secrets-restore-attempted"), "", 0o644)
	writeFile(t, fx.Path(".secrets", "proj", ".env"), "A=1\n", 0o600)

	for _, s := range steps.All() {
		filesBefore := homeFiles(t, fx.Home)
		envBefore, pathBefore, callsBefore := *fx.Env, os.Getenv("PATH"), len(fx.Fake.Calls())
		logBefore := readFile(t, steps.FollowupLogPath())
		s.Check(context.Background(), fx.Env)
		if !reflect.DeepEqual(*fx.Env, envBefore) {
			t.Errorf("%s: Check changed the Env: %+v -> %+v", s.Name, envBefore, *fx.Env)
		}
		if got := os.Getenv("PATH"); got != pathBefore {
			t.Errorf("%s: Check changed PATH: %q -> %q", s.Name, pathBefore, got)
		}
		if got := readFile(t, steps.FollowupLogPath()); got != logBefore {
			t.Errorf("%s: Check changed the follow-up log: %q -> %q", s.Name, logBefore, got)
		}
		if got := homeFiles(t, fx.Home); !reflect.DeepEqual(got, filesBefore) {
			t.Errorf("%s: Check changed files under HOME: %v -> %v", s.Name, filesBefore, got)
		}
		for _, c := range fx.Fake.Calls()[callsBefore:] {
			if mutatingCall(c) {
				t.Errorf("%s: Check ran a mutating command: %s %q", s.Name, c.Name, c.Args)
			}
		}
	}
}

// homeFiles lists every file under home, so a Check that removes or adds one shows.
func homeFiles(t *testing.T, home string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(home, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// mutatingCall reports a command that changes state, which no Check may run.
func mutatingCall(c run.Cmd) bool {
	switch c.Name {
	case "git":
		return containsSeq(c.Args, "worktree", "prune") || containsSeq(c.Args, "worktree", "add") ||
			containsSeq(c.Args, "worktree", "remove") || containsSeq(c.Args, "remote", "set-url")
	case "gh":
		return containsSeq(c.Args, "ssh-key", "add") || containsSeq(c.Args, "auth", "refresh") || containsSeq(c.Args, "config", "set")
	case "chezmoi":
		return len(c.Args) > 0 && (c.Args[0] == "init" || c.Args[0] == "apply")
	case "sh", "bash", "sudo":
		return true
	}
	return false
}

// B:240: worktrees leaked by a hard-killed earlier run are pruned once, by Setup.
func TestSetupPrunesWorktreesOnce(t *testing.T) {
	fx := newFixture(t)
	if err := steps.Setup(context.Background(), fx.Env); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range fx.Fake.Calls() {
		if c.Name == "git" && containsSeq(c.Args, "-C", fx.Repo, "worktree", "prune") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("git worktree prune ran %d times in Setup, want 1", n)
	}
}

// Y2: a follow-up log that cannot be emptied must not pass silently.
func TestSetupReturnsTruncationError(t *testing.T) {
	fx := newFixture(t)
	mustMkdir(t, steps.FollowupLogPath()) // a directory where the log should be
	if err := steps.Setup(context.Background(), fx.Env); err == nil {
		t.Error("Setup must return the error from emptying the follow-up log")
	}
}

// Y2: appendFollowup says so when it cannot open the log.
func TestAppendFollowupLogsWhenLogUnwritable(t *testing.T) {
	fx := newFixture(t)
	mustMkdir(t, steps.FollowupLogPath())
	for _, s := range steps.All() {
		if s.Name == "secrets-restore" {
			fx.Env.Ask = prompt.New(strings.NewReader("n\n"), io.Discard)
			if err := s.Apply(context.Background(), fx.Env); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !strings.Contains(fx.Log.String(), "follow-up") {
		t.Errorf("want a log line about the unwritable follow-up log, got %q", fx.Log.String())
	}
}

func TestSetupEmptiesFollowupLogAndExtendsPath(t *testing.T) {
	fx := newFixture(t)
	t.Setenv("PATH", "/usr/bin")
	writeFile(t, steps.FollowupLogPath(), "  • earlier note\n", 0o644)
	steps.Setup(context.Background(), fx.Env)
	steps.Setup(context.Background(), fx.Env) // idempotent
	if got := readFile(t, steps.FollowupLogPath()); got != "" {
		t.Errorf("follow-up log = %q, want empty", got)
	}
	want := "/opt/nanobrew/prefix/bin:" + fx.Path(".local", "bin") + ":/usr/bin"
	if got := os.Getenv("PATH"); got != want {
		t.Errorf("PATH = %q, want %q", got, want)
	}
}
