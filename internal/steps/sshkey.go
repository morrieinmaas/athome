package steps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/morrieinmaas/athome/internal/keys"
	"github.com/morrieinmaas/athome/internal/run"
)

func skKeyPath(ctx context.Context, env *Env) string {
	return filepath.Join(env.Home, ".ssh", dataHostname(ctx, env)+"_ed25519")
}

func skEnsureDir(env *Env) error {
	dir := filepath.Join(env.Home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}

// skDerive returns the public key of a private key file, from `ssh-keygen -y`.
func skDerive(ctx context.Context, env *Env, key string) (string, error) {
	res, err := env.Run.Run(ctx, run.Cmd{Name: "ssh-keygen", Args: []string{"-y", "-f", key}})
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		return "", fmt.Errorf("ssh-keygen -y -f %s failed: %s", key, strings.TrimSpace(res.Stderr))
	}
	return res.Stdout, nil
}

// skDerivePub writes <key>.pub from the private key with `ssh-keygen -y`.
func skDerivePub(ctx context.Context, env *Env, key string) error {
	pub, err := skDerive(ctx, env, key)
	if err != nil {
		return err
	}
	return os.WriteFile(key+".pub", []byte(pub), 0o644)
}

// skDerivePubInteractive is skDerivePub with the terminal attached, so ssh-keygen
// can ask for a passphrase, as in bash. Interactive output is not captured, so
// the key goes through a shell redirect into a temp file.
func skDerivePubInteractive(ctx context.Context, env *Env, key string) error {
	out := key + ".pub.tmp"
	defer os.Remove(out)
	res, err := env.Run.Run(ctx, run.Cmd{
		Name:        "sh",
		Args:        []string{"-c", `exec ssh-keygen -y -f "$1" > "$2"`, "athome-derive", key, out},
		Interactive: true,
	})
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("ssh-keygen -y -f %s failed", key)
	}
	pub, err := os.ReadFile(out)
	if err != nil || len(pub) == 0 {
		return fmt.Errorf("ssh-keygen -y -f %s gave no public key", key)
	}
	return os.WriteFile(key+".pub", pub, 0o644)
}

func skCopy(src, dst string, mode os.FileMode) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, b, mode); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}

// SSHKey returns the ssh key steps: restore from a backup dir (B:363-388) or
// Bitwarden (B:395-412), generate (B:422-453), regenerate a missing .pub, and
// copy a freshly generated key to Bitwarden (B:464-496).
func SSHKey() []Step {
	return []Step{
		{Name: "ssh-import-dir", Check: skImportDirCheck, Apply: skImportDirApply},
		{Name: "ssh-import-bw", Check: skImportBWCheck, Apply: skImportBWApply},
		{
			Name:  "ssh-keygen",
			Check: skKeygenCheck,
			Apply: func(ctx context.Context, env *Env) error {
				if !skDecide(ctx, env) {
					return nil
				}
				if err := skKeygenApply(ctx, env); err != nil {
					return err
				}
				env.KeyGenerated = true
				return nil
			},
		},
		{Name: "ssh-pub", Check: skPubCheck, Apply: skPubApply},
		{
			Name:  "ssh-bw-backup",
			Check: func(_ context.Context, env *Env) (bool, error) { return !env.KeyGenerated, nil },
			Apply: func(ctx context.Context, env *Env) error { return skBWBackup(ctx, env) },
		},
	}
}

// skBackupKey returns the first ssh/*_ed25519 (not .pub) in the import dir.
func skBackupKey(dir string) string {
	matches := globDir(filepath.Join(dir, "ssh"), "*_ed25519")
	sort.Strings(matches)
	for _, m := range matches {
		if !strings.HasSuffix(m, ".pub") {
			return m
		}
	}
	return ""
}

func skImportDirCheck(ctx context.Context, env *Env) (bool, error) {
	from := env.Opts.ImportFrom
	if from == "" {
		if env.Opts.ImportSSH {
			envLogln(env, "--import-ssh given without --import-from: nothing to import, a fresh SSH key is generated")
		}
		return true, nil
	}
	if fi, err := os.Stat(from); err != nil || !fi.IsDir() {
		return false, fmt.Errorf("--import-from: directory not found: %s", from)
	}
	if !env.Opts.ImportSSH {
		envLogln(env, "--import-from given without --import-ssh: nothing to import, a fresh SSH key is generated")
		return true, nil
	}
	src := skBackupKey(from)
	if src == "" {
		envLogln(env, "--import-from has no ssh/*_ed25519 key: nothing to import")
		return true, nil
	}
	return fileExists(skKeyPath(ctx, env)), nil
}

func skImportDirApply(ctx context.Context, env *Env) error {
	key := skKeyPath(ctx, env)
	src := skBackupKey(env.Opts.ImportFrom)
	if err := skEnsureDir(env); err != nil {
		return err
	}
	if err := skCopy(src, key, 0o600); err != nil {
		return err
	}
	if fileExists(src + ".pub") {
		if err := skCopy(src+".pub", key+".pub", 0o644); err != nil {
			return err
		}
	} else if err := skDerivePubInteractive(ctx, env, key); err != nil {
		skMarkUnderivable(env, key, err)
	}
	envLogln(env, "ssh key restored as %s", filepath.Base(key))
	return nil
}

func skImportBWCheck(ctx context.Context, env *Env) (bool, error) {
	item := env.Opts.ImportSSHBW
	if item == "" {
		return true, nil
	}
	if _, err := env.Run.LookPath("rbw"); err != nil {
		envLogln(env, "--import-ssh-bw: rbw not installed yet; install it, then re-run with --import-ssh-bw %s", item)
		return true, nil
	}
	// An existing key is never overwritten.
	key := skKeyPath(ctx, env)
	if fileExists(key) {
		envLogln(env, "existing key kept at %s; move it away and rerun to restore from Bitwarden", key)
		return true, nil
	}
	return false, nil
}

func skImportBWApply(ctx context.Context, env *Env) error {
	item := env.Opts.ImportSSHBW
	key := skKeyPath(ctx, env)
	if err := skEnsureDir(env); err != nil {
		return err
	}
	envLogln(env, "restoring SSH key from Bitwarden '%s' -> %s", item, filepath.Base(key))
	// Interactive: rbw asks for the master password on the terminal. Best effort, as bash.
	env.Run.Run(ctx, run.Cmd{Name: "rbw", Args: []string{"unlock"}, Interactive: true})
	res, err := env.Run.Run(ctx, run.Cmd{Name: "rbw", Args: []string{"get", "--field", "notes", item}})
	if err != nil || res.Code != 0 || res.Stdout == "" {
		envLogln(env, "couldn't read '%s' (run `rbw unlock`, check the item name)", item)
		return nil
	}
	// The note only becomes the key once ssh-keygen accepts it, so a bad note
	// leaves nothing behind and a rerun retries the import.
	tmp := key + ".import-tmp"
	os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	_, werr := f.WriteString(res.Stdout)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return werr
	}
	pub, err := skDerive(ctx, env, tmp)
	var deriveErr error
	if err != nil {
		// Only a passphrase key is worth a prompt; any other failure is a bad note.
		if !strings.Contains(err.Error(), "passphrase") {
			return err
		}
		defer os.Remove(tmp + ".pub")
		if deriveErr = skDerivePubInteractive(ctx, env, tmp); deriveErr == nil {
			b, rerr := os.ReadFile(tmp + ".pub")
			if rerr != nil {
				return rerr
			}
			pub = string(b)
		}
	}
	if err := os.Rename(tmp, key); err != nil {
		return err
	}
	if deriveErr != nil {
		// A valid key whose passphrase did not unlock stays; the marker keeps ssh-pub quiet.
		skMarkUnderivable(env, key, deriveErr)
		return nil
	}
	return os.WriteFile(key+".pub", []byte(pub), 0o644)
}

// skDecide asks the two ssh key questions when the answer is not preset and
// env.Ask is set; the answer is stored in Cfg.Data so later steps see it. It
// reports whether Apply should generate a key. Only Apply calls it, never a check.
func skDecide(ctx context.Context, env *Env) bool {
	if fileExists(skKeyPath(ctx, env)) {
		if env.Cfg.Data["sshKeyAction"] == "" && env.Ask != nil {
			skSetData(env, "sshKeyAction", env.Ask.Choose("ssh key "+filepath.Base(skKeyPath(ctx, env))+" found:", "use", "use", "generate", "reupload"))
		}
		return env.Cfg.Data["sshKeyAction"] == "generate"
	}
	if skGenerate(env) == "" && env.Ask != nil {
		answer := "true"
		if !env.Ask.Confirm("No ssh key for "+dataHostname(ctx, env)+": create one?", true) {
			answer = "false"
		}
		skSetData(env, "generateSshKey", answer)
	}
	if skGenerate(env) == "false" {
		envLogln(env, "no ssh key generated: bring your own / use the agent")
		return false
	}
	return true
}

// skGenerate is the generateSshKey answer; a remote box never generates one at init.
func skGenerate(env *Env) string {
	if env.Cfg.Machine == "remote" {
		return "false"
	}
	return env.Cfg.Data["generateSshKey"]
}

func skSetData(env *Env, key, val string) {
	if env.Cfg.Data == nil {
		env.Cfg.Data = map[string]string{}
	}
	env.Cfg.Data[key] = val
}

func skKeygenCheck(ctx context.Context, env *Env) (bool, error) {
	if fileExists(skKeyPath(ctx, env)) {
		if env.Ask != nil && env.Cfg.Data["sshKeyAction"] == "" {
			return false, nil // Apply asks
		}
		// ponytail: sshKeyAction=generate rotates the key on every run, as bash
		// does, so the "second run applies nothing" criterion excludes it.
		if env.Cfg.Data["sshKeyAction"] == "generate" {
			envLogln(env, "sshKeyAction=generate rotates this host's key on every run")
			return false, nil
		}
		return true, nil
	}
	if env.Ask != nil && skGenerate(env) == "" {
		return false, nil // Apply asks
	}
	if skGenerate(env) == "false" {
		envLogln(env, "no ssh key generated: bring your own / use the agent")
		return true, nil
	}
	return false, nil
}

func skKeygenApply(ctx context.Context, env *Env) error {
	host := dataHostname(ctx, env)
	key := skKeyPath(ctx, env)
	if err := skEnsureDir(env); err != nil {
		return err
	}
	if fileExists(key) {
		ts := time.Now().Format("20060102150405")
		envLogln(env, "backing up existing key (-> *.%s.bak) and generating a fresh one", ts)
		if err := os.Rename(key, key+"."+ts+".bak"); err != nil {
			return err
		}
		if fileExists(key + ".pub") {
			if err := os.Rename(key+".pub", key+".pub."+ts+".bak"); err != nil {
				return err
			}
		}
	}
	handle := env.Cfg.Data["githubHandle"]
	if handle == "" {
		if u, err := user.Current(); err == nil {
			handle = u.Username
		}
	}
	envLogln(env, "generating ssh key: %s", key)
	res, err := env.Run.Run(ctx, run.Cmd{Name: "ssh-keygen", Args: []string{"-t", "ed25519", "-f", key, "-N", "", "-C", handle + "@" + host}})
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("ssh-keygen failed: %s", strings.TrimSpace(res.Stderr))
	}
	return nil
}

// skPubMarker records a key whose .pub could not be derived (a passphrase key
// without a tty), so the next process does not apply ssh-pub again.
func skPubMarker(env *Env) string {
	return filepath.Join(env.Home, ".local", "state", "athome", "ssh-pub-underivable")
}

// skPubUnderivable reports whether the marker names key.
func skPubUnderivable(env *Env, key string) bool {
	b, err := os.ReadFile(skPubMarker(env))
	return err == nil && strings.TrimSpace(string(b)) == key
}

// skPubCheck: a private key without .pub gets its .pub back, decided from the
// files alone; Apply derives it. A key whose .pub cannot be derived is logged
// once and then counts as done, as in bash, so a rerun does not apply the step
// again.
func skPubCheck(ctx context.Context, env *Env) (bool, error) {
	key := skKeyPath(ctx, env)
	if !fileExists(key) || fileExists(key+".pub") || skPubUnderivable(env, key) {
		return true, nil
	}
	return false, nil
}

// skMarkUnderivable logs a failed derive and leaves the marker, so no later
// process applies ssh-pub again.
func skMarkUnderivable(env *Env, key string, cause error) {
	envLogln(env, "could not derive the public key: %v", cause)
	marker := skPubMarker(env)
	err := os.MkdirAll(filepath.Dir(marker), 0o755)
	if err == nil {
		err = os.WriteFile(marker, []byte(key+"\n"), 0o644)
	}
	if err != nil {
		envLogln(env, "could not record the underivable key in %s: %v", marker, err)
	}
}

// skPubApply writes the .pub from the private key. A failure only logs and
// leaves the marker; a derived .pub removes it.
func skPubApply(ctx context.Context, env *Env) error {
	key := skKeyPath(ctx, env)
	if err := skDerivePub(ctx, env, key); err != nil {
		skMarkUnderivable(env, key, err)
		return nil
	}
	os.Remove(skPubMarker(env))
	return nil
}

// skPubMarkerTidy drops the marker once the key it names has a .pub again. It
// reads the key path from the marker, so Setup needs no hostname lookup.
func skPubMarkerTidy(env *Env) {
	b, err := os.ReadFile(skPubMarker(env))
	if err != nil {
		return
	}
	if key := strings.TrimSpace(string(b)); fileExists(key + ".pub") {
		os.Remove(skPubMarker(env))
	}
}

// skBWBackup ports B:464-496. It never fails: without an unlocked rbw it
// prints the manual commands. bwBackupSshKey=false skips it; empty or true
// keeps the copy.
func skBWBackup(ctx context.Context, env *Env) error {
	if env.Cfg.Data["bwBackupSshKey"] == "false" { // answered in the form: no copy, no hint
		return nil
	}
	key := skKeyPath(ctx, env)
	item := "ssh/" + filepath.Base(key)
	if _, err := env.Run.LookPath("rbw"); err == nil {
		err := skStoreInRBW(ctx, env, key, item)
		if err == nil {
			envLogln(env, "stored in Bitwarden: %s", item)
			return nil
		}
		if !errors.Is(err, keys.ErrLocked) {
			// An `rbw add` hint here could make the duplicate item Store refuses.
			envLogln(env, "could not store %s in Bitwarden: %v", item, err)
			return nil
		}
	}
	envLogln(env, "back up this machine's SSH key to Bitwarden (recommended): once rbw is unlocked run\n    rbw add '%s'   # or rbw edit if it already exists; line 1: ssh-key-backup, line 2+: paste %s\nRestore it elsewhere with: --import-ssh-bw %s", item, filepath.Base(key), item)
	return nil
}

func skStoreInRBW(ctx context.Context, env *Env, key, item string) error {
	priv, err := os.ReadFile(key)
	if err != nil {
		return err
	}
	return keys.Store(ctx, env.Run, item, keys.SSHKeyPassword, strings.TrimRight(string(priv), "\n"))
}
