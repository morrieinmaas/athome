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

// nanobrewReady points the nanobrew paths at a temp /opt/nb link and a
// user-writable prefix, the state the package-manager script leaves behind.
func nanobrewReady(fx *fixture) (link, prefix string) {
	fx.T.Helper()
	prefix = fx.Path("nanobrew-prefix")
	mustMkdir(fx.T, prefix)
	link = fx.Path("opt-nb")
	if err := os.Symlink(prefix, link); err != nil {
		fx.T.Fatal(err)
	}
	fx.T.Cleanup(steps.SetNanobrewPaths(link, prefix))
	return link, prefix
}

func rpmHave(fx *fixture, missing string) {
	for _, p := range []string{"dnf-plugins-core", "rpmfusion-free-release", "rpmfusion-nonfree-release"} {
		code := 0
		if p == missing {
			code = 1
		}
		fx.Fake.On("rpm", "-q", p).Return(run.Result{Code: code})
	}
}

func TestPackageManagerCheck(t *testing.T) {
	cases := []struct {
		name, os, distro string
		setup            func(fx *fixture)
	}{
		{"darwin nb on PATH", "darwin", "", func(fx *fixture) { fx.Fake.Have("nb"); nanobrewReady(fx) }},
		{"darwin nb installed", "darwin", "", func(fx *fixture) {
			nb := fx.Path("nb")
			writeFile(fx.T, nb, "#!/bin/sh\n", 0o755)
			fx.T.Cleanup(steps.SetNanobrewBin(nb))
			nanobrewReady(fx)
		}},
		{"fedora plugins and rpmfusion", "linux", "fedora", func(fx *fixture) { rpmHave(fx, "") }},
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
			t.Cleanup(steps.SetNanobrewPaths(fx.Path("absent-link"), fx.Path("absent-prefix")))
			rpmHave(fx, "dnf-plugins-core")
			fx.Fake.On("rpm", "-q", "rpmfusion-free-release").Return(run.Result{Code: 1})
			if !appliedSet(fx.MustExec(steps.Baseline()))["baseline-package-manager"] {
				t.Errorf("package manager missing, must be applied:\n%s", fx.dump())
			}
		})
	}
}

// The script also links /opt/nb and keeps the prefix user-writable (B:938, the
// template), so nb on PATH alone is not "done".
func TestPackageManagerCheckDarwinNeedsLinkAndWritablePrefix(t *testing.T) {
	t.Run("no /opt/nb link", func(t *testing.T) {
		fx := newFixture(t)
		fx.Env.OS = "darwin"
		fx.Fake.Have("nb")
		_, prefix := nanobrewReady(fx)
		t.Cleanup(steps.SetNanobrewPaths(fx.Path("no-such-link"), prefix))
		if !appliedSet(fx.MustExec(steps.Baseline()))["baseline-package-manager"] {
			t.Errorf("a missing /opt/nb link must re-run the script:\n%s", fx.dump())
		}
	})
	t.Run("prefix not writable", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("root can write anywhere")
		}
		fx := newFixture(t)
		fx.Env.OS = "darwin"
		fx.Fake.Have("nb")
		_, prefix := nanobrewReady(fx)
		if err := os.Chmod(prefix, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(prefix, 0o700) })
		if !appliedSet(fx.MustExec(steps.Baseline()))["baseline-package-manager"] {
			t.Errorf("a root-owned prefix must re-run the script:\n%s", fx.dump())
		}
	})
}

// The script installs dnf-plugins-core and both RPM Fusion releases; all three
// must be present for the step to be done.
func TestPackageManagerCheckFedoraNeedsAllThree(t *testing.T) {
	for _, missing := range []string{"dnf-plugins-core", "rpmfusion-free-release", "rpmfusion-nonfree-release"} {
		t.Run("missing "+missing, func(t *testing.T) {
			fx := newFixture(t)
			fx.Env.OS, fx.Env.Distro = "linux", "fedora"
			rpmHave(fx, missing)
			if !appliedSet(fx.MustExec(steps.Baseline()))["baseline-package-manager"] {
				t.Errorf("without %s the script must run:\n%s", missing, fx.dump())
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
