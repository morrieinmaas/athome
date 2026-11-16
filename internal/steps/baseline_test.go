package steps_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
)

func chezmoiInit(fx *fixture) []string {
	return []string{"init", "--apply", "--force", "--exclude=scripts", "--source=" + filepath.Join(fx.Repo, "home")}
}

func TestChezmoiApplyNonFatal(t *testing.T) {
	t.Run("failure adds a warning, no error", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.On("chezmoi", chezmoiInit(fx)...).Return(run.Result{Code: 1})
		if _, err := fx.Exec(steps.Baseline()); err != nil {
			t.Fatalf("a failed chezmoi init must not abort: %v", err)
		}
		if fx.Env.Warnings < 1 {
			t.Errorf("Warnings = %d, want at least 1", fx.Env.Warnings)
		}
		got, _ := os.ReadFile(steps.FollowupLogPath())
		if !strings.Contains(string(got), "chezmoi init exited 1 during bootstrap") {
			t.Errorf("want the chezmoi init follow-up line (B:921), log: %q", got)
		}
	})
	t.Run("success runs the documented command and adds none", func(t *testing.T) {
		fx := newFixture(t)
		fx.MustExec(steps.Baseline())
		if !fx.called("chezmoi", chezmoiInit(fx)...) {
			t.Errorf("want `chezmoi %s`:\n%s", strings.Join(chezmoiInit(fx), " "), fx.dump())
		}
		if fx.Env.Warnings != 0 {
			t.Errorf("Warnings = %d, want 0", fx.Env.Warnings)
		}
	})
}

// The apply runs inside a non-interactive env: chezmoi's scripts and mise need
// these (B:775-801).
func TestChezmoiApplyEnv(t *testing.T) {
	fx := newFixture(t)
	fx.Fake.Have("gh")
	fx.Fake.On("gh", "auth", "token").Return(run.Result{Stdout: "tok123\n"})
	fx.MustExec(steps.Baseline())
	calls := fx.callsTo("chezmoi", chezmoiInit(fx)...)
	if len(calls) != 1 {
		t.Fatalf("chezmoi init not recorded:\n%s", fx.dump())
	}
	env := strings.Join(calls[0].Env, "\n")
	for _, want := range []string{"ATHOME_BOOTSTRAP=1", "GITHUB_TOKEN=tok123", "MISE_GITHUB_TOKEN=tok123"} {
		if !strings.Contains(env, want) {
			t.Errorf("chezmoi init env lacks %s: %v", want, calls[0].Env)
		}
	}
}

func chezmoiVerify(fx *fixture) []string {
	return []string{"verify", "--source=" + filepath.Join(fx.Repo, "home"), "--exclude=scripts"}
}

func appliedSet(rep steps.Report) map[string]bool {
	m := map[string]bool{}
	for _, n := range rep.Applied {
		m[n] = true
	}
	return m
}

// Criterion 7: a second run applies neither chezmoi-apply nor the package manager
// once they are in place.
func TestChezmoiApplyCheck(t *testing.T) {
	t.Run("verify passes: skipped, no init", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.On("chezmoi", chezmoiVerify(fx)...).Return(run.Result{Code: 0})
		rep := fx.MustExec(steps.Baseline())
		if appliedSet(rep)["chezmoi-apply"] || fx.called("chezmoi", "init") {
			t.Errorf("a verified home must not be applied again:\n%s", fx.dump())
		}
	})
	t.Run("verify fails: applied", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.On("chezmoi", chezmoiVerify(fx)...).Return(run.Result{Code: 1})
		rep := fx.MustExec(steps.Baseline())
		if !appliedSet(rep)["chezmoi-apply"] {
			t.Errorf("a differing home must be applied:\n%s", fx.dump())
		}
	})
	// bash always re-runs chezmoi init after seeding chezmoi.toml (B:769-922).
	t.Run("data applied, verify passes: applied", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.On("chezmoi", chezmoiVerify(fx)...).Return(run.Result{Code: 0})
		rep := fx.MustExec(append(steps.Data(), steps.Baseline()...))
		if !appliedSet(rep)["chezmoi-data"] || !appliedSet(rep)["chezmoi-apply"] {
			t.Errorf("seeding chezmoi.toml must force the chezmoi init:\n%s", fx.dump())
		}
	})
}

func TestChezmoiInitAndInstallsAreInteractive(t *testing.T) {
	fx := newFixture(t)
	fx.Env.OS, fx.Env.Distro = "darwin", ""
	t.Cleanup(steps.SetNanobrewBin(fx.Path("absent-nb")))
	fx.MustExec(steps.Baseline())
	if c := fx.callsTo("chezmoi", chezmoiInit(fx)...); len(c) != 1 || !c[0].Interactive {
		t.Errorf("chezmoi init --apply must be interactive: %+v", c)
	}
	if c := fx.callsTo("nb", "install"); len(c) != 1 || !c[0].Interactive {
		t.Errorf("the package install must be interactive: %+v", c)
	}
}

// B:769-770: the follow-up log is truncated before chezmoi-apply, even when the
// apply itself is skipped, so notes from an old run never show up again.
func TestFollowupLogTruncated(t *testing.T) {
	fx := newFixture(t)
	t.Setenv("TMPDIR", t.TempDir())
	log := steps.FollowupLogPath()
	writeFile(t, log, "  * stale note\n", 0o644)
	fx.Fake.On("chezmoi", chezmoiVerify(fx)...).Return(run.Result{Code: 0})
	fx.MustExec(steps.Baseline())
	if got := readFile(t, log); got != "" {
		t.Errorf("follow-up log = %q, want it truncated", got)
	}
	if !strings.HasSuffix(log, "athome-bootstrap-followup.log") {
		t.Errorf("log path %q", log)
	}
}

func TestChezmoiApplyPassesFollowupLog(t *testing.T) {
	fx := newFixture(t)
	t.Setenv("TMPDIR", t.TempDir())
	fx.MustExec(steps.Baseline())
	calls := fx.callsTo("chezmoi", chezmoiInit(fx)...)
	if len(calls) != 1 || !strings.Contains(strings.Join(calls[0].Env, "\n"), "ATHOME_FOLLOWUP_LOG="+steps.FollowupLogPath()) {
		t.Errorf("chezmoi init env lacks the follow-up log: %+v", calls)
	}
}

// F1: with --ref, chezmoi reads the worktree's home/, not the checkout's.
func TestBaselineUsesRefSource(t *testing.T) {
	fx := newFixture(t)
	src := filepath.Join(t.TempDir(), "wt", "home")
	script := "#!/bin/bash\n# pm from the worktree\n"
	writeFile(t, filepath.Join(src, ".chezmoiscripts", "run_once_before_01-install-package-manager.sh.tmpl"), script, 0o644)
	fx.Env.Source = src
	fx.Fake.On("chezmoi", "verify", "--source="+src, "--exclude=scripts").Return(run.Result{Code: 1})
	fx.Fake.On("chezmoi", "execute-template").Return(run.Result{Stdout: "echo pm\n"})
	fx.MustExec(steps.Baseline())
	if !fx.called("chezmoi", "--exclude=scripts", "--source="+src) {
		t.Errorf("chezmoi init must use --source=%s:\n%s", src, fx.dump())
	}
	if !fx.called("chezmoi", "verify", "--source="+src) {
		t.Errorf("chezmoi verify must use --source=%s:\n%s", src, fx.dump())
	}
	var rendered bool
	for _, c := range fx.callsTo("chezmoi", "execute-template") {
		rendered = rendered || c.Stdin == script
	}
	if !rendered {
		t.Errorf("the pm script must be read from the worktree source:\n%s", fx.dump())
	}
}

const templateChanged = "config file template has changed, run chezmoi init to regenerate config file\n"

// S1: chezmoi verify exits 0 even when the config template changed; the apply
// must not be skipped then.
func TestChezmoiApplyCheckConfigTemplate(t *testing.T) {
	for _, stream := range []string{"stdout", "stderr"} {
		t.Run("warning on "+stream, func(t *testing.T) {
			fx := newFixture(t)
			res := run.Result{Code: 0}
			if stream == "stdout" {
				res.Stdout = templateChanged
			} else {
				res.Stderr = templateChanged
			}
			fx.Fake.On("chezmoi", chezmoiVerify(fx)...).Return(res)
			if !appliedSet(fx.MustExec(steps.Baseline()))["chezmoi-apply"] {
				t.Errorf("a changed config template must be applied:\n%s", fx.dump())
			}
		})
	}
	setup := func(t *testing.T, tmplAge time.Duration) *fixture {
		fx := newFixture(t)
		fx.Fake.On("chezmoi", chezmoiVerify(fx)...).Return(run.Result{Code: 0})
		tmpl := filepath.Join(fx.Repo, "home", ".chezmoi.toml.tmpl")
		cfg := fx.Path(".config", "chezmoi", "chezmoi.toml")
		writeFile(t, tmpl, "x", 0o644)
		writeFile(t, cfg, "x", 0o644)
		base := time.Now().Add(-time.Hour)
		if err := os.Chtimes(cfg, base, base); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(tmpl, base.Add(tmplAge), base.Add(tmplAge)); err != nil {
			t.Fatal(err)
		}
		return fx
	}
	t.Run("template newer than chezmoi.toml: applied", func(t *testing.T) {
		fx := setup(t, time.Minute)
		if !appliedSet(fx.MustExec(steps.Baseline()))["chezmoi-apply"] {
			t.Errorf("a template newer than chezmoi.toml must be applied:\n%s", fx.dump())
		}
	})
	t.Run("template older than chezmoi.toml: skipped", func(t *testing.T) {
		fx := setup(t, -time.Minute)
		if appliedSet(fx.MustExec(steps.Baseline()))["chezmoi-apply"] {
			t.Errorf("an up-to-date config must not be applied again:\n%s", fx.dump())
		}
	})
}
