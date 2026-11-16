package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/morrieinmaas/athome/internal/run"
)

const (
	pmScript          = "run_once_before_01-install-package-manager.sh.tmpl"
	projectDirsScript = "run_03-setup-project-dirs.sh.tmpl"
	baselinePkgs      = "gh rbw gitleaks"
	nanobrewBinDir    = "/opt/nanobrew/prefix/bin"
)

var (
	nanobrewBin    = filepath.Join(nanobrewBinDir, "nb")
	nanobrewLink   = "/opt/nb" // the short prefix nb relocates binaries to
	nanobrewPrefix = "/opt/nanobrew/prefix"
)

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

// FollowupLogPath is where chezmoi's scripts append manual-step notes (B:769).
// Like bash it lives in $TMPDIR.
func FollowupLogPath() string {
	return filepath.Join(tmpDir(), "athome-bootstrap-followup.log")
}

func baselineWarn(env *Env, format string, a ...any) {
	env.Warnings++
	envLogf(env, "warning: "+format+"\n", a...)
}

// appendFollowup adds a bullet to the follow-up log unless it is already there.
func appendFollowup(line string) {
	path := FollowupLogPath()
	if b, err := os.ReadFile(path); err == nil && strings.Contains(string(b), line) {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "  • %s\n", line)
}

// baselineDepWarn is baselineWarn plus B:950's follow-up line, written once.
func baselineDepWarn(env *Env, format string, a ...any) {
	baselineWarn(env, format, a...)
	appendFollowup("a baseline dep (package manager / gh / rbw / project dirs / mise) failed; re-run `mise run bootstrap`.")
}

// chezmoiApply is B:769-922: a failed init is a warning, never an error.
func chezmoiApply(ctx context.Context, env *Env) error {
	env.DataChanged = false // this init consumes the seeded chezmoi.toml
	environ := append(os.Environ(),
		"ATHOME_FOLLOWUP_LOG="+FollowupLogPath(),
		"ATHOME_BOOTSTRAP=1",
		"PATH="+nanobrewBinDir+":"+os.Getenv("PATH"),
	)
	if _, err := env.Run.LookPath("gh"); err == nil {
		res, err := env.Run.Run(ctx, run.Cmd{Name: "gh", Args: []string{"auth", "token"}})
		if tok := strings.TrimSpace(res.Stdout); err == nil && res.Code == 0 && tok != "" {
			environ = append(environ, "GITHUB_TOKEN="+tok, "MISE_GITHUB_TOKEN="+tok)
			envLogf(env, "mise will use gh's GitHub token\n")
		}
	}
	source := sourceDir(env)
	res, err := env.Run.Run(ctx, run.Cmd{
		Name:        "chezmoi",
		Args:        []string{"init", "--apply", "--force", "--exclude=scripts", "--source=" + source},
		Env:         environ,
		Interactive: true,
	})
	if err != nil {
		baselineWarn(env, "chezmoi init could not run: %v; re-run `mise run apply` to finish", err)
		appendFollowup("chezmoi init could not run during bootstrap; re-run `mise run apply` (idempotent) to finish.")
	} else if res.Code != 0 {
		baselineWarn(env, "chezmoi init exited %d; bootstrap continues, re-run `mise run apply` to finish", res.Code)
		appendFollowup(fmt.Sprintf("chezmoi init exited %d during bootstrap; re-run `mise run apply` (idempotent) to finish.", res.Code))
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
func renderAndRun(ctx context.Context, env *Env, name string) error {
	tmpl, err := os.ReadFile(filepath.Join(sourceDir(env), ".chezmoiscripts", name))
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	rendered, err := env.Run.Run(ctx, run.Cmd{Name: "chezmoi", Args: []string{"execute-template"}, Stdin: string(tmpl)})
	if err != nil || rendered.Code != 0 {
		return fmt.Errorf("render %s failed", name)
	}
	if rendered.Stdout == "" {
		return nil
	}
	res, err := env.Run.Run(ctx, run.Cmd{Name: "bash", Stdin: rendered.Stdout, Interactive: true, NeedsSudo: name == pmScript})
	if err != nil || res.Code != 0 {
		return fmt.Errorf("%s exited non-zero", name)
	}
	return nil
}

func packageManager(ctx context.Context, env *Env) error {
	if err := renderAndRun(ctx, env, pmScript); err != nil {
		baselineDepWarn(env, "baseline package manager: %v", err)
	}
	return nil
}

// pmInstallCmd mirrors scripts/lib/pm.sh.
func pmInstallCmd(env *Env, pkgs ...string) run.Cmd {
	switch {
	case env.OS == "darwin":
		return run.Cmd{Name: "nb", Args: append([]string{"install"}, pkgs...), NeedsSudo: true}
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
	return run.Cmd{Name: "yay", Args: args, NeedsSudo: true}
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
	install := pmInstallCmd(env, strings.Fields(baselinePkgs)...)
	install.Interactive = true
	res, err := env.Run.Run(ctx, install)
	if err != nil || res.Code != 0 {
		baselineDepWarn(env, "baseline packages (%s) failed; re-run bootstrap", baselinePkgs)
	}
	return nil
}

// projectDirContexts lists the dirs run_03-setup-project-dirs.sh.tmpl seeds per
// machine type; only the done check needs them, the script does the seeding.
func projectDirContexts(machine string) []string {
	if machine == "work" {
		return []string{"work"}
	}
	return []string{"personal", "sidebiz"}
}

func projectDirsDone(_ context.Context, env *Env) (bool, error) {
	for _, d := range projectDirContexts(env.Cfg.Machine) {
		if _, err := os.Stat(filepath.Join(env.Home, d, ".envrc")); err != nil {
			return false, nil
		}
	}
	return true, nil
}

func projectDirs(ctx context.Context, env *Env) error {
	if err := renderAndRun(ctx, env, projectDirsScript); err != nil {
		baselineDepWarn(env, "baseline project dirs: %v", err)
	}
	return nil
}

func miseDone(_ context.Context, env *Env) (bool, error) {
	_, err := env.Run.LookPath("mise")
	return err == nil, nil
}

func installMise(ctx context.Context, env *Env) error {
	res, err := env.Run.Run(ctx, run.Cmd{
		Name:        "sh",
		Args:        []string{"-c", "curl -fsSL https://mise.run | MISE_INSTALL_HELP=0 sh"},
		Interactive: true,
	})
	if err != nil || res.Code != 0 {
		baselineDepWarn(env, "mise install failed; re-run bootstrap")
	}
	return nil
}

// chezmoiApplyDone: the home dir already matches the source (exit 0 of verify)
// and chezmoi.toml is current: verify exits 0 even when the config template
// changed, so that warning, or a template newer than chezmoi.toml, is not done.
func chezmoiApplyDone(ctx context.Context, env *Env) (bool, error) {
	// B:770: notes from an earlier run must not bleed into this one, even when
	// the apply is skipped.
	os.WriteFile(FollowupLogPath(), nil, 0o644)
	if env.DataChanged {
		return false, nil
	}
	res, err := env.Run.Run(ctx, run.Cmd{
		Name: "chezmoi",
		Args: []string{"verify", "--source=" + sourceDir(env), "--exclude=scripts"},
	})
	if err != nil || res.Code != 0 {
		return false, nil
	}
	if strings.Contains(res.Stdout+res.Stderr, "config file template has changed") {
		return false, nil
	}
	return !configTemplateNewer(env), nil
}

func configTemplateNewer(env *Env) bool {
	tmpl, err := os.Stat(filepath.Join(sourceDir(env), ".chezmoi.toml.tmpl"))
	if err != nil {
		return false
	}
	cfg, err := os.Stat(filepath.Join(env.Home, ".config", "chezmoi", "chezmoi.toml"))
	return err != nil || tmpl.ModTime().After(cfg.ModTime())
}

// nanobrewInstalled: nb is on PATH or in nanobrew's own bin dir.
func nanobrewInstalled(env *Env) bool {
	if _, err := env.Run.LookPath("nb"); err == nil {
		return true
	}
	_, err := os.Stat(nanobrewBin)
	return err == nil
}

// packageManagerDone: the OS's package manager is already set up, as far as the
// pm script (run_once_before_01) sets it up.
func packageManagerDone(ctx context.Context, env *Env) (bool, error) {
	prependBaselinePath(env)
	switch {
	case env.OS == "darwin":
		if !nanobrewInstalled(env) {
			return false, nil
		}
		// The script also links /opt/nb and keeps the prefix user-writable.
		if _, err := os.Stat(nanobrewLink); err != nil {
			return false, nil
		}
		return syscall.Access(nanobrewPrefix, 2) == nil, nil // 2 = W_OK
	case env.Distro == "fedora":
		for _, pkg := range []string{"dnf-plugins-core", "rpmfusion-free-release", "rpmfusion-nonfree-release"} {
			res, err := env.Run.Run(ctx, run.Cmd{Name: "rpm", Args: []string{"-q", pkg}})
			if err != nil || res.Code != 0 {
				return false, nil
			}
		}
		return true, nil
	}
	_, err := env.Run.LookPath("yay")
	return err == nil, nil
}

// prependBaselinePath ports B:935: a just-installed nb (and mise) must be
// findable by the rest of the run, so put their dirs first on PATH.
func prependBaselinePath(env *Env) {
	front := nanobrewBinDir + string(os.PathListSeparator) + filepath.Join(env.Home, ".local", "bin")
	path := os.Getenv("PATH")
	if path == front || strings.HasPrefix(path, front+string(os.PathListSeparator)) {
		return
	}
	os.Setenv("PATH", front+string(os.PathListSeparator)+path)
}
