package steps_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
)

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

func TestPackagesDarwinNoSudo(t *testing.T) {
	fx := newFixture(t)
	t.Cleanup(steps.SetNanobrewBin(filepath.Join(t.TempDir(), "nb")))
	fx.Env.OS, fx.Env.Distro = "darwin", ""
	r, stop := steps.WarmSudo(fx.Fake, fx.Log)
	defer stop()
	fx.Env.Run = r
	fx.MustExec(steps.Baseline())
	if n := sudoVCount(fx.Fake); n != 0 {
		t.Errorf("darwin nb install must not prime sudo, got %d sudo -v:\n%s", n, fx.dump())
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
	got, _ := os.ReadFile(steps.FollowupLogPath())
	if !strings.Contains(string(got), "a baseline dep (package manager / gh / rbw / project dirs / mise) failed") {
		t.Errorf("want the baseline dep follow-up line (B:950), log: %q", got)
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
		{"fedora dnf and plugins-core", "linux", "fedora", func(fx *fixture) { fx.Fake.Have("dnf"); rpmHave(fx, "") }},
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

// The script guarantees dnf and dnf-plugins-core; RPM Fusion is best effort in
// it (mirror down, or already enabled), so only the first two gate the step.
func TestPackageManagerCheckFedoraNeedsDnfAndPluginsCore(t *testing.T) {
	cases := []struct {
		name    string
		haveDnf bool
		missing string
		applied bool
	}{
		{"dnf missing", false, "", true},
		{"dnf-plugins-core missing", true, "dnf-plugins-core", true},
		{"rpmfusion-free missing", true, "rpmfusion-free-release", false},
		{"rpmfusion-nonfree missing", true, "rpmfusion-nonfree-release", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fx := newFixture(t)
			fx.Env.OS, fx.Env.Distro = "linux", "fedora"
			if c.haveDnf {
				fx.Fake.Have("dnf")
			}
			rpmHave(fx, c.missing)
			if got := appliedSet(fx.MustExec(steps.Baseline()))["baseline-package-manager"]; got != c.applied {
				t.Errorf("applied = %v, want %v:\n%s", got, c.applied, fx.dump())
			}
		})
	}
}

// After the script runs, a still missing RPM Fusion release is a plain log
// line (bash parity): no warning, no follow-up entry, never a failure.
func TestPackageManagerFedoraLogsWhenRPMFusionMissing(t *testing.T) {
	fx := newFixture(t)
	fx.Env.OS, fx.Env.Distro = "linux", "fedora"
	rpmHave(fx, "rpmfusion-nonfree-release")
	if _, err := fx.Exec(steps.Baseline()); err != nil {
		t.Fatalf("a missing RPM Fusion release must not fail the run: %v", err)
	}
	if !strings.Contains(fx.Log.String(), "rpmfusion-nonfree-release") {
		t.Errorf("want a log line naming rpmfusion-nonfree-release:\n%s", fx.Log.String())
	}
	if fx.Env.Warnings != 0 {
		t.Errorf("Warnings = %d, a missing RPM Fusion release only logs", fx.Env.Warnings)
	}
	got, _ := os.ReadFile(steps.FollowupLogPath())
	if strings.Contains(string(got), "rpmfusion") || strings.Contains(string(got), "baseline dep") {
		t.Errorf("a missing RPM Fusion release adds no follow-up entry, log: %q", got)
	}
	fx = newFixture(t)
	fx.Env.OS, fx.Env.Distro = "linux", "fedora"
	rpmHave(fx, "")
	fx.MustExec(steps.Baseline())
	if fx.Env.Warnings != 0 {
		t.Errorf("both releases present: Warnings = %d, want 0", fx.Env.Warnings)
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
