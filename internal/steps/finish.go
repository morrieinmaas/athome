package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/morrieinmaas/athome/internal/run"
)

// Finish ports the tail of bootstrap.sh (B:951-1048): sourceDir pin, repo git
// hooks, local secrets home, secrets restore and the detached-HEAD recovery.
// Build 1 has no prompts, so Bitwarden setup (bw-setup) never runs.
func Finish() []Step {
	return []Step{
		{Name: "source-dir-pin", Check: sourceDirPinned, Apply: pinSourceDir},
		{Name: "git-hooks", Check: hooksDone, Apply: installHooks},
		{Name: "secrets-dir", Check: secretsDirDone, Apply: makeSecretsDir},
		{Name: "secrets-restore", Check: secretsRestoreDone, Apply: restoreSecrets},
		{Name: "secrets-restore-hint", Check: secretsHintDone, Apply: secretsHint},
		{Name: "checkout-main", Check: notDetached, Apply: checkoutMain},
	}
}

func chezmoiConfigPath(env *Env) string {
	return filepath.Join(env.Home, ".config", "chezmoi", "chezmoi.toml")
}

// B:958-964: pin only an existing config that has no sourceDir line.
func sourceDirPinned(_ context.Context, env *Env) (bool, error) {
	b, err := os.ReadFile(chezmoiConfigPath(env))
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "sourceDir") {
			return true, nil
		}
	}
	return false, nil
}

func pinSourceDir(_ context.Context, env *Env) error {
	p := chezmoiConfigPath(env)
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	src := filepath.Join(env.RepoRoot, "home")
	if !utf8.ValidString(src) {
		return fmt.Errorf("the source path %q is not valid UTF-8, so chezmoi.toml cannot pin it; move the repo to a UTF-8 path", src)
	}
	out := []byte(fmt.Sprintf("sourceDir = \"%s\"\n", tomlStr(src)))
	out = append(out, b...)
	tmp := p + ".new"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func hookPath(env *Env) string {
	return filepath.Join(env.RepoRoot, ".git", "hooks", "pre-commit")
}

// B:969-974: only when .git exists and pre-commit is absent.
func hooksDone(_ context.Context, env *Env) (bool, error) {
	if fi, err := os.Stat(filepath.Join(env.RepoRoot, ".git")); err != nil || !fi.IsDir() {
		return true, nil
	}
	return fileExists(hookPath(env)), nil
}

func installHooks(_ context.Context, env *Env) error {
	srcDir := filepath.Join(env.RepoRoot, "home", "dot_config", "git", "hooks")
	dstDir := filepath.Join(env.RepoRoot, ".git", "hooks")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	for _, h := range []string{"pre-commit", "commit-msg"} {
		b, err := os.ReadFile(filepath.Join(srcDir, "executable_"+h))
		if err != nil {
			return err
		}
		dst := filepath.Join(dstDir, h)
		if err := os.WriteFile(dst, b, 0o755); err != nil {
			return err
		}
		if err := os.Chmod(dst, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// B:982-1008: ~/.secrets is always created; bw-setup is never run without a terminal.
func secretsDirDone(_ context.Context, env *Env) (bool, error) {
	fi, err := os.Stat(filepath.Join(env.Home, ".secrets"))
	return err == nil && fi.IsDir() && fi.Mode().Perm() == 0o700, nil
}

func makeSecretsDir(_ context.Context, env *Env) error {
	p := filepath.Join(env.Home, ".secrets")
	if err := os.MkdirAll(p, 0o700); err != nil {
		return err
	}
	return os.Chmod(p, 0o700)
}

func rbwUnlocked(ctx context.Context, env *Env) bool {
	if _, err := env.Run.LookPath("rbw"); err != nil {
		return false
	}
	r, err := env.Run.Run(ctx, run.Cmd{Name: "rbw", Args: []string{"unlocked"}})
	return err == nil && r.Code == 0
}

func restoreScript(env *Env) string {
	return filepath.Join(env.Home, ".local", "bin", "secrets-restore")
}

type secretsState int

const (
	secretsNothing    secretsState = iota // rbw locked or missing, or the env files exist
	secretsRestorable                     // the script can run
	secretsNoScript                       // restorable, but the script is not there yet
)

const secretsFollowup = "Secrets: run `secrets-restore` to recover ~/.config env files from the encrypted vault. See docs/secrets.md."

// B:1015-1030: restore only when rbw is unlocked, no ~/.secrets/*/.env exists
// and the secrets-restore script is executable.
func secretsStateOf(ctx context.Context, env *Env) secretsState {
	if !rbwUnlocked(ctx, env) {
		return secretsNothing
	}
	if hasSecretsEnv(env) {
		return secretsNothing
	}
	fi, err := os.Stat(restoreScript(env))
	if err != nil || fi.Mode().Perm()&0o111 == 0 {
		return secretsNoScript
	}
	return secretsRestorable
}

// restoreMarker records an attempted restore that left no ~/.secrets/*/.env, so
// a rerun does not prompt for the vault again.
func restoreMarker(env *Env) string {
	return filepath.Join(env.Home, ".local", "state", "athome", "secrets-restore-attempted")
}

// globDir lists the entries of dir whose names match pattern, sorted, as full
// paths. dir is read literally, so a `[` in it is not a pattern.
func globDir(dir, pattern string) []string {
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		if ok, _ := filepath.Match(pattern, e.Name()); ok {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

func hasSecretsEnv(env *Env) bool {
	for _, d := range globDir(filepath.Join(env.Home, ".secrets"), "*") {
		if fileExists(filepath.Join(d, ".env")) {
			return true
		}
	}
	return false
}

// secretsMarkerTidy drops the restore marker once a ~/.secrets/*/.env exists.
// Setup runs it, so the check only reads.
func secretsMarkerTidy(env *Env) {
	if hasSecretsEnv(env) {
		os.Remove(restoreMarker(env))
	}
}

func secretsRestoreDone(ctx context.Context, env *Env) (bool, error) {
	if hasSecretsEnv(env) {
		return true, nil
	}
	if fileExists(restoreMarker(env)) {
		return true, nil
	}
	return secretsStateOf(ctx, env) != secretsRestorable, nil
}

// secretsHintDone: the follow-up line is wanted only without the script, and
// once per run (Setup empties the log).
func secretsHintDone(ctx context.Context, env *Env) (bool, error) {
	return secretsStateOf(ctx, env) != secretsNoScript || followupHas(secretsFollowup), nil
}

func secretsHint(_ context.Context, env *Env) error {
	appendFollowup(env, secretsFollowup)
	return nil
}

func restoreSecrets(ctx context.Context, env *Env) error {
	r, err := env.Run.Run(ctx, run.Cmd{Name: restoreScript(env), Interactive: true})
	if err != nil || r.Code != 0 {
		envLogf(env, "  secrets-restore had issues; run it manually later.\n")
	}
	if hasSecretsEnv(env) {
		os.Remove(restoreMarker(env))
		return nil
	}
	marker := restoreMarker(env)
	err = os.MkdirAll(filepath.Dir(marker), 0o755)
	if err == nil {
		err = os.WriteFile(marker, nil, 0o644)
	}
	if err != nil {
		envLogf(env, "could not record the restore attempt in %s: %v\n", marker, err)
	}
	return nil
}

func gitRepo(env *Env, args ...string) run.Cmd {
	return run.Cmd{Name: "git", Args: append([]string{"-C", env.RepoRoot}, args...)}
}

// B:1038-1052: a detached HEAD in the repo goes back to main.
func notDetached(ctx context.Context, env *Env) (bool, error) {
	r, err := env.Run.Run(ctx, gitRepo(env, "rev-parse", "--abbrev-ref", "HEAD"))
	if err != nil || r.Code != 0 {
		return true, nil
	}
	return strings.TrimSpace(r.Stdout) != "HEAD", nil
}

func checkoutMain(ctx context.Context, env *Env) error {
	ok := func(args ...string) bool {
		r, err := env.Run.Run(ctx, gitRepo(env, args...))
		return err == nil && r.Code == 0
	}
	envLogf(env, "%s is in detached HEAD; restoring to main\n", env.RepoRoot)
	ok("fetch", "origin", "--quiet")
	switch {
	case ok("checkout", "main"):
		envLogf(env, "restored to main\n")
	case ok("checkout", "-B", "main", "origin/main"):
		envLogf(env, "created local main tracking origin/main\n")
	default:
		envLogf(env, "could not restore to main; check `git status` in %s\n", env.RepoRoot)
	}
	return nil
}
