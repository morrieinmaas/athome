package steps_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestPackages(t *testing.T) {
	cases := []struct {
		name, os, distro string
		name0            string
		prefix           []string
		pkgs             []string
	}{
		{"darwin", "darwin", "", "nb", []string{"install"}, []string{"gh", "rbw", "gitleaks"}},
		{"fedora", "linux", "fedora", "sudo", []string{"dnf", "install", "-y"}, []string{"gh", "rbw", "gitleaks"}},
		{"arch", "linux", "arch", "yay", []string{"-S", "--needed"}, []string{"github-cli", "rbw", "gitleaks"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fx := newFixture(t)
			fx.Env.OS, fx.Env.Distro = c.os, c.distro
			fx.MustExec(steps.Baseline())
			var found bool
			for _, cmd := range fx.Fake.Calls() {
				if cmd.Name != c.name0 || !containsSeq(cmd.Args, c.prefix...) {
					continue
				}
				n := len(cmd.Args)
				if n >= len(c.pkgs) && strings.Join(cmd.Args[n-len(c.pkgs):], " ") == strings.Join(c.pkgs, " ") {
					found = true
				}
			}
			if !found {
				t.Errorf("want %s %v ... %v, recorded:\n%s", c.name0, c.prefix, c.pkgs, fx.dump())
			}
		})
	}
}

func TestPackagesFailureIsAWarning(t *testing.T) {
	fx := newFixture(t)
	fx.Env.OS, fx.Env.Distro = "darwin", ""
	fx.Fake.On("nb", "install", "gh", "rbw", "gitleaks").Return(run.Result{Code: 1})
	if _, err := fx.Exec(steps.Baseline()); err != nil {
		t.Fatalf("a failed package install must not abort: %v", err)
	}
	if fx.Env.Warnings < 1 {
		t.Errorf("Warnings = %d, want at least 1", fx.Env.Warnings)
	}
}

func TestPackageManagerScriptViaTemplate(t *testing.T) {
	fx := newFixture(t)
	script := "#!/bin/bash\n# pm {{ .machineType }}\n"
	writeFile(t, filepath.Join(fx.Repo, "home", ".chezmoiscripts", "run_once_before_01-install-package-manager.sh.tmpl"), script, 0o644)
	fx.Fake.On("chezmoi", "execute-template").Return(run.Result{Stdout: "echo rendered-pm\n"})
	fx.MustExec(steps.Baseline())
	var rendered, piped bool
	for _, c := range fx.Fake.Calls() {
		if c.Name == "chezmoi" && containsSeq(c.Args, "execute-template") && c.Stdin == script {
			rendered = true
		}
		if c.Stdin == "echo rendered-pm\n" {
			piped = true
		}
	}
	if !rendered || !piped {
		t.Errorf("the package-manager script must be rendered by chezmoi (%v) and its output run (%v):\n%s", rendered, piped, fx.dump())
	}
}

func TestProjectDirsDone(t *testing.T) {
	fx := newFixture(t)
	fx.Env.Cfg.Machine = "personal"
	writeFile(t, fx.Path("personal", ".envrc"), "x", 0o644)
	if appliedSet(fx.MustExec(steps.Baseline()))["baseline-project-dirs"] != true {
		t.Fatal("sidebiz is missing, the step must apply")
	}
	writeFile(t, fx.Path("sidebiz", ".envrc"), "x", 0o644)
	if appliedSet(fx.MustExec(steps.Baseline()))["baseline-project-dirs"] {
		t.Error("both .envrc present, the step must be skipped")
	}
}

func TestMiseBinary(t *testing.T) {
	t.Run("skipped when mise is on PATH", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.Have("mise")
		fx.MustExec(steps.Baseline())
		if fx.mentions("mise.run") {
			t.Errorf("mise is present, installer must not run:\n%s", fx.dump())
		}
	})
	t.Run("installed when missing", func(t *testing.T) {
		fx := newFixture(t)
		fx.MustExec(steps.Baseline())
		if !fx.mentions("mise.run") {
			t.Errorf("mise missing, installer from mise.run expected:\n%s", fx.dump())
		}
	})
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
}

func TestPackageManagerCheck(t *testing.T) {
	cases := []struct {
		name, os, distro string
		setup            func(fx *fixture)
	}{
		{"darwin nb on PATH", "darwin", "", func(fx *fixture) { fx.Fake.Have("nb") }},
		{"darwin nb installed", "darwin", "", func(fx *fixture) {
			nb := fx.Path("nb")
			writeFile(fx.T, nb, "#!/bin/sh\n", 0o755)
			fx.T.Cleanup(steps.SetNanobrewBin(nb))
		}},
		{"fedora rpmfusion", "linux", "fedora", func(fx *fixture) {
			fx.Fake.On("rpm", "-q", "rpmfusion-free-release").Return(run.Result{Code: 0})
		}},
		{"arch yay", "linux", "arch", func(fx *fixture) { fx.Fake.Have("yay") }},
	}
	for _, c := range cases {
		t.Run(c.name+" is done", func(t *testing.T) {
			fx := newFixture(t)
			fx.Env.OS, fx.Env.Distro = c.os, c.distro
			if c.os == "darwin" {
				t.Cleanup(steps.SetNanobrewBin(fx.Path("absent-nb")))
			}
			c.setup(fx)
			if appliedSet(fx.MustExec(steps.Baseline()))["baseline-package-manager"] {
				t.Errorf("package manager already set up, must be skipped:\n%s", fx.dump())
			}
		})
	}
	for _, c := range cases {
		t.Run(c.name+" missing is applied", func(t *testing.T) {
			fx := newFixture(t)
			fx.Env.OS, fx.Env.Distro = c.os, c.distro
			t.Cleanup(steps.SetNanobrewBin(fx.Path("absent-nb")))
			fx.Fake.On("rpm", "-q", "rpmfusion-free-release").Return(run.Result{Code: 1})
			if !appliedSet(fx.MustExec(steps.Baseline()))["baseline-package-manager"] {
				t.Errorf("package manager missing, must be applied:\n%s", fx.dump())
			}
		})
	}
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
	log := steps.FollowupLogPath(fx.Home)
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
	if len(calls) != 1 || !strings.Contains(strings.Join(calls[0].Env, "\n"), "ATHOME_FOLLOWUP_LOG="+steps.FollowupLogPath(fx.Home)) {
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

// F6: after the package manager step, nb and mise are findable by later steps.
func TestBaselineExtendsPath(t *testing.T) {
	fx := newFixture(t)
	t.Setenv("PATH", "/usr/bin")
	fx.MustExec(steps.Baseline())
	want := "/opt/nanobrew/prefix/bin:" + filepath.Join(fx.Home, ".local", "bin") + ":/usr/bin"
	if got := os.Getenv("PATH"); got != want {
		t.Errorf("PATH = %q, want %q", got, want)
	}
}

// F19: a missing baseline script is a warning, not silence.
func TestMissingBaselineScriptWarns(t *testing.T) {
	fx := newFixture(t)
	if err := os.Remove(filepath.Join(fx.Repo, "home", ".chezmoiscripts", "run_once_before_01-install-package-manager.sh.tmpl")); err != nil {
		t.Fatal(err)
	}
	fx.MustExec(steps.Baseline())
	if fx.Env.Warnings < 1 || !strings.Contains(fx.Log.String(), "run_once_before_01-install-package-manager.sh.tmpl") {
		t.Errorf("a missing script must warn (Warnings=%d):\n%s", fx.Env.Warnings, fx.Log)
	}
}

// F18: the project dirs come from the repo's own script, not a Go copy.
func TestProjectDirsRendersTheRepoScript(t *testing.T) {
	fx := newFixture(t)
	script := "# dirs {{ .machineType }}\n"
	writeFile(t, filepath.Join(fx.Repo, "home", ".chezmoiscripts", "run_03-setup-project-dirs.sh.tmpl"), script, 0o644)
	fx.Fake.On("chezmoi", "execute-template").Return(run.Result{Stdout: "echo dirs\n"})
	fx.MustExec(steps.Baseline())
	var piped bool
	for _, c := range fx.Fake.Calls() {
		piped = piped || (c.Name == "bash" && c.Stdin == "echo dirs\n")
	}
	if !piped || exists(fx.Path("personal", ".envrc")) {
		t.Errorf("the rendered script must run via bash, and Go must not seed dirs itself (piped=%v):\n%s", piped, fx.dump())
	}
}
