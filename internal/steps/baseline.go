package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/morrieinmaas/athome/internal/run"
)

const (
	pmScript       = "run_once_before_01-install-package-manager.sh.tmpl"
	baselinePkgs   = "gh rbw gitleaks"
	nanobrewBinDir = "/opt/nanobrew/prefix/bin"
)

var nanobrewBin = filepath.Join(nanobrewBinDir, "nb")

// Baseline ports B:758-951: chezmoi init --apply (config files only), then the
// baseline deps bootstrap's own later steps need. Failures here are warnings.
func Baseline() []Step {
	return []Step{
		{Name: "chezmoi-apply", Check: chezmoiApplyDone, Apply: chezmoiApply},
		{Name: "baseline-package-manager", Check: packageManagerDone, Apply: packageManager},
		{Name: "baseline-packages", Check: packagesDone, Apply: installPackages},
		{Name: "baseline-project-dirs", Check: projectDirsDone, Apply: projectDirs},
		{Name: "baseline-mise", Check: miseDone, Apply: installMise},
	}
}

func baselineLog(env *Env, format string, a ...any) {
	if env.Log != nil {
		fmt.Fprintf(env.Log, format, a...)
	}
}

func baselineWarn(env *Env, format string, a ...any) {
	env.Warnings++
	baselineLog(env, "warning: "+format+"\n", a...)
}

// chezmoiApply is B:769-922: a failed init is a warning, never an error.
func chezmoiApply(ctx context.Context, env *Env) error {
	environ := append(os.Environ(),
		"ATHOME_FOLLOWUP_LOG="+filepath.Join(tmpDir(), "athome-bootstrap-followup.log"),
		"ATHOME_BOOTSTRAP=1",
		"PATH="+nanobrewBinDir+":"+os.Getenv("PATH"),
	)
	if _, err := env.Run.LookPath("gh"); err == nil {
		res, err := env.Run.Run(ctx, run.Cmd{Name: "gh", Args: []string{"auth", "token"}})
		if tok := strings.TrimSpace(res.Stdout); err == nil && res.Code == 0 && tok != "" {
			environ = append(environ, "GITHUB_TOKEN="+tok, "MISE_GITHUB_TOKEN="+tok)
			baselineLog(env, "mise will use gh's GitHub token\n")
		}
	}
	source := filepath.Join(env.RepoRoot, "home")
	res, err := env.Run.Run(ctx, run.Cmd{
		Name: "chezmoi",
		Args: []string{"init", "--apply", "--force", "--exclude=scripts", "--source=" + source},
		Env:  environ,
	})
	if err != nil {
		baselineWarn(env, "chezmoi init could not run: %v; re-run `mise run apply` to finish", err)
	} else if res.Code != 0 {
		baselineWarn(env, "chezmoi init exited %d; bootstrap continues, re-run `mise run apply` to finish", res.Code)
	}
	return nil
}

func tmpDir() string {
	if d := os.Getenv("TMPDIR"); d != "" {
		return d
	}
	return "/tmp"
}

// renderAndRun is B:933/941: render a chezmoi script template, pipe it to bash.
// A script that is not in the repo is skipped.
func renderAndRun(ctx context.Context, env *Env, name string) error {
	tmpl, err := os.ReadFile(filepath.Join(env.RepoRoot, "home", ".chezmoiscripts", name))
	if err != nil {
		return nil
	}
	rendered, err := env.Run.Run(ctx, run.Cmd{Name: "chezmoi", Args: []string{"execute-template"}, Stdin: string(tmpl)})
	if err != nil || rendered.Code != 0 {
		return fmt.Errorf("render %s failed", name)
	}
	if rendered.Stdout == "" {
		return nil
	}
	res, err := env.Run.Run(ctx, run.Cmd{Name: "bash", Stdin: rendered.Stdout})
	if err != nil || res.Code != 0 {
		return fmt.Errorf("%s exited non-zero", name)
	}
	return nil
}

func packageManager(ctx context.Context, env *Env) error {
	if err := renderAndRun(ctx, env, pmScript); err != nil {
		baselineWarn(env, "baseline package manager: %v", err)
	}
	return nil
}

// pmInstallCmd mirrors scripts/lib/pm.sh.
func pmInstallCmd(env *Env, pkgs ...string) run.Cmd {
	switch {
	case env.OS == "darwin":
		return run.Cmd{Name: "nb", Args: append([]string{"install"}, pkgs...)}
	case env.Distro == "fedora":
		return run.Cmd{Name: "sudo", Args: append([]string{"dnf", "install", "-y"}, pkgs...)}
	}
	args := []string{"-S", "--needed", "--noconfirm", "--answerdiff", "None", "--answerclean", "None"}
	for _, p := range pkgs {
		if p == "gh" {
			p = "github-cli"
		}
		args = append(args, p)
	}
	return run.Cmd{Name: "yay", Args: args}
}

func packagesDone(_ context.Context, env *Env) (bool, error) {
	for _, b := range strings.Fields(baselinePkgs) {
		if _, err := env.Run.LookPath(b); err != nil {
			return false, nil
		}
	}
	return true, nil
}

func installPackages(ctx context.Context, env *Env) error {
	res, err := env.Run.Run(ctx, pmInstallCmd(env, strings.Fields(baselinePkgs)...))
	if err != nil || res.Code != 0 {
		baselineWarn(env, "baseline packages (%s) failed; re-run bootstrap", baselinePkgs)
	}
	return nil
}

// projectDirContexts mirrors run_03-setup-project-dirs.sh.tmpl.
func projectDirContexts(machine string) [][2]string {
	if machine == "work" {
		return [][2]string{{"work", "work"}}
	}
	return [][2]string{{"personal", "personal"}, {"sidebiz", "sidebiz"}}
}

func projectDirsDone(_ context.Context, env *Env) (bool, error) {
	for _, d := range projectDirContexts(env.Cfg.Machine) {
		if _, err := os.Stat(filepath.Join(env.Home, d[0], ".envrc")); err != nil {
			return false, nil
		}
	}
	return true, nil
}

func projectDirs(_ context.Context, env *Env) error {
	for _, d := range projectDirContexts(env.Cfg.Machine) {
		dir := filepath.Join(env.Home, d[0])
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		envrc := filepath.Join(dir, ".envrc")
		if _, err := os.Stat(envrc); err == nil {
			continue
		}
		body := fmt.Sprintf("# Auto-seeded by chezmoi (run_03-setup-project-dirs.sh.tmpl).\n"+
			"# Bundles the right rbw/Bitwarden context for anything under %s/ -\n"+
			"# subdirectories inherit unless they override with their own .envrc.\n"+
			"use ctx %s\n", dir, d[1])
		if err := os.WriteFile(envrc, []byte(body), 0o644); err != nil {
			return err
		}
		baselineLog(env, "seeded %s\n", envrc)
	}
	return nil
}

func miseDone(_ context.Context, env *Env) (bool, error) {
	_, err := env.Run.LookPath("mise")
	return err == nil, nil
}

func installMise(ctx context.Context, env *Env) error {
	res, err := env.Run.Run(ctx, run.Cmd{
		Name: "sh",
		Args: []string{"-c", "curl -fsSL https://mise.run | MISE_INSTALL_HELP=0 sh"},
	})
	if err != nil || res.Code != 0 {
		baselineWarn(env, "mise install failed; re-run bootstrap")
	}
	return nil
}

// chezmoiApplyDone: the home dir already matches the source (exit 0 of verify).
func chezmoiApplyDone(ctx context.Context, env *Env) (bool, error) {
	res, err := env.Run.Run(ctx, run.Cmd{
		Name: "chezmoi",
		Args: []string{"verify", "--source=" + filepath.Join(env.RepoRoot, "home"), "--exclude=scripts"},
	})
	return err == nil && res.Code == 0, nil
}

// packageManagerDone: the OS's package manager is already set up.
func packageManagerDone(ctx context.Context, env *Env) (bool, error) {
	switch {
	case env.OS == "darwin":
		if _, err := env.Run.LookPath("nb"); err == nil {
			return true, nil
		}
		_, err := os.Stat(nanobrewBin)
		return err == nil, nil
	case env.Distro == "fedora":
		res, err := env.Run.Run(ctx, run.Cmd{Name: "rpm", Args: []string{"-q", "rpmfusion-free-release"}})
		return err == nil && res.Code == 0, nil
	}
	_, err := env.Run.LookPath("yay")
	return err == nil, nil
}
