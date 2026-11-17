package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/secrets"
)

// Finish ports the tail of bootstrap-legacy.sh (B:951-1048): sourceDir pin, repo git
// hooks, local secrets home, secrets restore and the detached-HEAD recovery.
// Bitwarden setup (bw-setup) runs only with env.Ask set, never without a terminal.
func Finish() []Step {
	return []Step{
		{Name: "source-dir-pin", Check: sourceDirPinned, Apply: pinSourceDir},
		{Name: "git-hooks", Check: hooksDone, Apply: installHooks},
		{Name: "secrets-dir", Check: secretsDirDone, Apply: makeSecretsDir},
		{Name: "bitwarden-setup", Check: bitwardenDone, Apply: bitwardenSetup},
		{Name: "secrets-restore", Check: secretsRestoreDone, Apply: restoreSecrets},
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

const secretsFollowup = "Secrets: run `athome secrets restore` to recover ~/.config env files from the encrypted vault. See docs/secrets.md."

// secretsRestorable: rbw is unlocked and no env file has been restored yet.
func secretsRestorable(ctx context.Context, env *Env) bool {
	return rbwUnlocked(ctx, env) && !hasSecretsEnv(env)
}

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
	// An interactive rerun asks again (B:1023-1025); the marker only quiets
	// non-interactive runs.
	if env.Ask == nil && fileExists(restoreMarker(env)) {
		return true, nil
	}
	return !secretsRestorable(ctx, env), nil
}

// restoreSecrets runs athome secrets restore after asking (an interactive run) and
// records the attempt in a marker unless a ~/.secrets/*/.env now exists. Declining
// queues the follow-up hint instead.
func restoreSecrets(ctx context.Context, env *Env) error {
	if !presetConfirm(env, "restoreSecrets", "restore your encrypted secrets now (athome secrets restore)?") { // B:1025
		appendFollowup(env, secretsFollowup)
		return nil
	}
	if secrets.Restore(ctx, secrets.FromEnv(env.Home, env.Run, env.Log, env.Log)) != 0 {
		envLogf(env, "  athome secrets restore had issues; run it manually later.\n")
	}
	if hasSecretsEnv(env) {
		os.Remove(restoreMarker(env))
		return nil
	}
	writeRestoreMarker(env)
	return nil
}

func writeRestoreMarker(env *Env) {
	marker := restoreMarker(env)
	err := os.MkdirAll(filepath.Dir(marker), 0o755)
	if err == nil {
		err = os.WriteFile(marker, nil, 0o644)
	}
	if err != nil {
		envLogf(env, "could not record the restore attempt in %s: %v\n", marker, err)
	}
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

func bwSetupScript(env *Env) string { return filepath.Join(env.Home, ".local", "bin", "bw-setup") }

// rbwDeviceID is the file rbw writes once onboarded (B:984-987).
func rbwDeviceID(env *Env) string {
	if env.OS == "darwin" {
		return filepath.Join(env.Home, "Library", "Application Support", "rbw", "device_id")
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		base = filepath.Join(env.Home, ".local", "share")
	}
	return filepath.Join(base, "rbw", "device_id")
}

func bwSetupRunnable(env *Env) bool {
	fi, err := os.Stat(bwSetupScript(env))
	return err == nil && fi.Mode().Perm()&0o111 != 0
}

// bitwardenDone: nothing to do without a terminal or with Bitwarden switched off.
// An unset useBitwarden is asked on every interactive run (B:990). Once chosen,
// it is done when rbw is unlocked or registered, or when there is no script to
// run. It never prompts.
func bitwardenDone(ctx context.Context, env *Env) (bool, error) {
	if env.Ask == nil || env.Cfg.Data["useBitwarden"] == "false" {
		return true, nil
	}
	if env.Cfg.Data["useBitwarden"] == "" {
		return false, nil
	}
	if rbwUnlocked(ctx, env) || fileExists(rbwDeviceID(env)) {
		return true, nil
	}
	return !bwSetupRunnable(env), nil
}

// bitwardenSetup ports B:990-1008, the interactive path.
func bitwardenSetup(ctx context.Context, env *Env) error {
	if env.Cfg.Data["useBitwarden"] == "" && !env.Ask.Confirm("Use Bitwarden (rbw) for secrets sync?", true) { // B:990
		envLogf(env, "local-only secrets, Bitwarden skipped. Populate ~/.secrets/<repo>/.env by hand,\n")
		envLogf(env, "  or run `bw-setup` later to sync from Bitwarden.\n")
		return nil
	}
	switch {
	case rbwUnlocked(ctx, env):
		envLogf(env, "Bitwarden already unlocked, rbw get/list ready.\n")
	case fileExists(rbwDeviceID(env)):
		envLogf(env, "Bitwarden already set up here (device registered), locked; `rbw unlock` when needed.\n")
	case bwSetupRunnable(env):
		if r, err := env.Run.Run(ctx, run.Cmd{Name: bwSetupScript(env), Interactive: true}); err != nil || r.Code != 0 {
			env.Warnings++
			envLogf(env, "  Bitwarden login didn't complete, see above. ~/.secrets is ready for local use meanwhile.\n")
			appendFollowup(env, "Bitwarden: `bw-setup` did NOT finish (often missing pinentry / API key). Re-run `bw-setup` once fixed. docs/secrets.md.")
		}
	default:
		envLogf(env, "local-only secrets for now, run `bw-setup` in a terminal to sync Bitwarden later.\n")
	}
	return nil
}

// presetConfirm answers a default-yes confirm from env.Cfg.Data[key] when it is
// "true" or "false"; otherwise it asks, and without a terminal it is yes.
func presetConfirm(env *Env, key, q string) bool {
	switch env.Cfg.Data[key] {
	case "true":
		return true
	case "false":
		return false
	}
	return env.Ask == nil || env.Ask.Confirm(q, true)
}
