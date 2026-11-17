package steps

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/secrets"
)

// RestoreAll is the init list for a replacement laptop: the baseline installs
// rbw, gh and bw-setup, the four restore steps bring back Bitwarden, gh, the
// ssh key and the vault, and only then do the usual ssh key steps run, so no
// new key is made while the old one can still be restored.
func RestoreAll() []Step {
	var all []Step
	for _, g := range [][]Step{Prepare(), Data(), Baseline(), restoreSteps(), SSHKey(), GitHub(), Finish()} {
		all = append(all, g...)
	}
	return all
}

func restoreSteps() []Step {
	return []Step{
		{Name: "restore-bitwarden", Check: restoreBWCheck, Apply: restoreBWApply},
		{Name: "restore-gh", Check: restoreGHCheck, Apply: restoreGHApply},
		{Name: "restore-ssh-key", Check: restoreKeyCheck, Apply: restoreKeyApply},
		{Name: "restore-secrets", Check: restoreSecretsCheck, Apply: restoreSecretsApply},
	}
}

func restoreBWCheck(ctx context.Context, env *Env) (bool, error) {
	return rbwUnlocked(ctx, env), nil
}

func restoreBWApply(ctx context.Context, env *Env) error {
	if env.Ask != nil && bwSetupRunnable(env) {
		env.Run.Run(ctx, run.Cmd{Name: bwSetupScript(env), Interactive: true})
		if rbwUnlocked(ctx, env) {
			return nil
		}
	}
	return fmt.Errorf("rbw is not unlocked: run bw-setup, then re-run athome init --restore")
}

func ghLoggedIn(ctx context.Context, env *Env) bool {
	res, err := env.Run.Run(ctx, run.Cmd{Name: "gh", Args: []string{"auth", "status"}})
	return err == nil && res.Code == 0
}

func restoreGHCheck(ctx context.Context, env *Env) (bool, error) {
	return ghLoggedIn(ctx, env), nil
}

func restoreGHApply(ctx context.Context, env *Env) error {
	if env.Ask != nil {
		env.Run.Run(ctx, run.Cmd{Name: "gh", Args: []string{"auth", "login"}, Interactive: true})
		if ghLoggedIn(ctx, env) {
			return nil
		}
	}
	return fmt.Errorf("gh is not logged in: run gh auth login, then re-run athome init --restore")
}

// restoreKeyPresent counts ~/.ssh/id_ed25519 as the laptop key, as keys.LaptopKey does.
func restoreKeyPresent(ctx context.Context, env *Env) bool {
	return fileExists(skKeyPath(ctx, env)) || fileExists(filepath.Join(env.Home, ".ssh", "id_ed25519"))
}

func restoreKeyCheck(ctx context.Context, env *Env) (bool, error) {
	return restoreKeyPresent(ctx, env), nil
}

var sshItem = regexp.MustCompile(`^ssh/[^/]+_ed25519$`)

// restoreItem picks the Bitwarden item holding the old laptop key.
func restoreItem(ctx context.Context, env *Env) (string, error) {
	if item := env.Opts.ImportSSHBW; item != "" {
		return item, nil
	}
	res, err := env.Run.Run(ctx, run.Cmd{Name: "rbw", Args: []string{"list"}})
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		return "", fmt.Errorf("rbw list failed: %s", strings.TrimSpace(res.Stderr))
	}
	var items []string
	for _, l := range strings.Split(res.Stdout, "\n") {
		if l = strings.TrimSpace(l); sshItem.MatchString(l) {
			items = append(items, l)
		}
	}
	switch {
	case len(items) == 1:
		return items[0], nil
	case len(items) == 0:
		return "", fmt.Errorf("no ssh key item in Bitwarden: expected ssh/<old-host>_ed25519; pass --import-ssh-bw <item> if it is named differently")
	case env.Ask != nil:
		return env.Ask.Choose("which ssh key item holds your old laptop key?", items[0], items...), nil
	}
	return "", fmt.Errorf("several ssh key items in Bitwarden (%s); pick one with --import-ssh-bw <item>", strings.Join(items, ", "))
}

func restoreKeyApply(ctx context.Context, env *Env) error {
	item, err := restoreItem(ctx, env)
	if err != nil {
		return err
	}
	prev := env.Opts.ImportSSHBW
	env.Opts.ImportSSHBW = item
	// The ssh-import-bw step must not log "existing key kept" for a key restored here.
	defer func() { env.Opts.ImportSSHBW = prev }()
	if err := skImportBWApply(ctx, env); err != nil {
		return err
	}
	key := skKeyPath(ctx, env)
	if !fileExists(key) {
		return fmt.Errorf("could not restore %s from Bitwarden: no key written to %s", item, key)
	}
	skSetData(env, "sshKeyAction", "use")
	res, err := env.Run.Run(ctx, run.Cmd{Name: "chezmoi", Args: []string{"apply"}, Interactive: true})
	if err != nil || res.Code != 0 {
		envLogln(env, "chezmoi apply did not finish; run it to pick up the restored key in the signing config")
	}
	return nil
}

func restoreSecretsCheck(_ context.Context, env *Env) (bool, error) {
	return hasSecretsEnv(env), nil
}

func restoreSecretsApply(ctx context.Context, env *Env) error {
	if secrets.Restore(ctx, secrets.FromEnv(env.Home, env.Run, env.Log, env.Log)) != 0 {
		return fmt.Errorf("athome secrets restore failed; fix it, then re-run athome init --restore")
	}
	return nil
}
