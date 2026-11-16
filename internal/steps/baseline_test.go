package steps_test

import (
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

func TestProjectDirs(t *testing.T) {
	t.Run("personal", func(t *testing.T) {
		fx := newFixture(t)
		fx.Env.Cfg.Machine = "personal"
		fx.MustExec(steps.Baseline())
		for dir, ctx := range map[string]string{"personal": "personal", "sidebiz": "sidebiz"} {
			if got := readFile(t, fx.Path(dir, ".envrc")); !strings.Contains(got, "use ctx "+ctx) {
				t.Errorf("~/%s/.envrc = %q, want `use ctx %s`", dir, got, ctx)
			}
		}
		if exists(fx.Path("work")) {
			t.Errorf("a personal machine gets no ~/work")
		}
	})
	t.Run("work", func(t *testing.T) {
		fx := newFixture(t)
		fx.Env.Cfg.Machine = "work"
		fx.MustExec(steps.Baseline())
		if got := readFile(t, fx.Path("work", ".envrc")); !strings.Contains(got, "use ctx work") {
			t.Errorf("~/work/.envrc = %q, want `use ctx work`", got)
		}
		if exists(fx.Path("personal")) || exists(fx.Path("sidebiz")) {
			t.Errorf("a work machine gets neither ~/personal nor ~/sidebiz")
		}
	})
	t.Run("an existing .envrc is untouched", func(t *testing.T) {
		fx := newFixture(t)
		writeFile(t, fx.Path("personal", ".envrc"), "# mine\n", 0o644)
		fx.MustExec(steps.Baseline())
		if got := readFile(t, fx.Path("personal", ".envrc")); got != "# mine\n" {
			t.Errorf(".envrc clobbered: %q", got)
		}
		if !exists(fx.Path("sidebiz", ".envrc")) {
			t.Errorf("the other dir is still seeded")
		}
	})
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
