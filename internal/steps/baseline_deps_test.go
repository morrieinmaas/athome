package steps_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
)

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

// S2/S4: the scripts read their prompts from the terminal, so they run interactive.
func TestBaselineScriptsAreInteractive(t *testing.T) {
	fx := newFixture(t)
	fx.Fake.On("chezmoi", "execute-template").Return(run.Result{Stdout: "echo rendered\n"})
	fx.MustExec(steps.Baseline())
	bashes := 0
	for _, c := range fx.Fake.Calls() {
		if c.Name == "bash" && c.Stdin == "echo rendered\n" {
			bashes++
			if !c.Interactive {
				t.Errorf("a rendered script must run interactive: %+v", c)
			}
		}
	}
	if bashes != 2 {
		t.Errorf("want 2 rendered scripts run, got %d:\n%s", bashes, fx.dump())
	}
	var mise []run.Cmd
	for _, c := range fx.Fake.Calls() {
		if c.Name == "sh" && strings.Contains(strings.Join(c.Args, " "), "mise.run") {
			mise = append(mise, c)
		}
	}
	if len(mise) != 1 || !mise[0].Interactive {
		t.Errorf("the mise installer must be interactive: %+v", mise)
	}
}

// V3: same hole as the chezmoi installer: a failed download exits 0 through
// the pipe, so a missing mise afterwards is a failed baseline dep (B:946-950).
func TestMiseInstallMustLeaveABinary(t *testing.T) {
	fx := newFixture(t)
	fx.Env.Run = fx.Fake
	if _, err := fx.Exec(steps.Baseline()); err != nil {
		t.Fatalf("a failed mise install must not abort: %v", err)
	}
	if fx.Env.Warnings < 1 {
		t.Errorf("Warnings = %d, want at least 1", fx.Env.Warnings)
	}
	got, _ := os.ReadFile(steps.FollowupLogPath())
	if !strings.Contains(string(got), "a baseline dep (package manager / gh / rbw / project dirs / mise) failed") {
		t.Errorf("want the baseline dep follow-up line, log: %q", got)
	}
}

// Z7: bash's one shared deps_rc makes any number of failed baseline deps one
// warning (B:948-951).
func TestBaselineDepFailuresCountAsOneWarning(t *testing.T) {
	fx := newFixture(t)
	fx.Env.OS, fx.Env.Distro = "darwin", ""
	fx.Fake.On("nb", "install", "gh", "rbw", "gitleaks").Return(run.Result{Code: 1})
	fx.Env.Run = fx.Fake // no mise either
	if _, err := fx.Exec(steps.Baseline()); err != nil {
		t.Fatalf("failed deps must not abort: %v", err)
	}
	if n := strings.Count(fx.Log.String(), "warning: "); n < 2 {
		t.Fatalf("want at least two dep failures logged, got %d:\n%s", n, fx.Log)
	}
	if fx.Env.Warnings != 1 {
		t.Errorf("Warnings = %d, want 1 for any number of failed deps", fx.Env.Warnings)
	}
}
