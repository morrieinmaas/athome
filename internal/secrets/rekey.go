package secrets

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/morrieinmaas/athome/internal/keys"
	"github.com/morrieinmaas/athome/internal/prompt"
	"github.com/morrieinmaas/athome/internal/run"
)

// RekeyOpts is Opts plus what Rekey needs to decide about asking. TTY is the
// caller's terminal detection; Rekey never looks for a terminal itself.
type RekeyOpts struct {
	Opts
	Yes bool
	TTY bool
	In  io.Reader        // answers to the confirmation prompt
	Now func() time.Time // nil means time.Now
}

// Rekey makes a new vault repo with a new git-crypt key under the same name,
// renaming the old repo out of the way. It returns the process exit code.
func Rekey(ctx context.Context, o RekeyOpts) int {
	now := time.Now
	if o.Now != nil {
		now = o.Now
	}
	date := now().Format("20060102")

	if !o.Yes && !o.TTY {
		o.errf("athome secrets rekey: --yes is required without a terminal\n")
		return exitAbort
	}
	if code := o.preconditions(ctx); code != exitOK {
		return code
	}
	owner := o.owner(ctx)
	if owner == "" {
		o.errf("cannot work out the vault owner; set ATHOME_GITHUB_HANDLE or SECRETS_VAULT_REPO, or log in with `gh auth login`\n")
		return exitError
	}
	oldName := o.Name + "-old-" + date
	slug := owner + "/" + o.Name
	oldItem := "git-crypt/" + o.Name + "-old-" + date + "-key"
	if code := o.clashes(ctx, owner, oldName, oldItem); code != exitOK {
		return code
	}

	if !o.Yes {
		q := fmt.Sprintf("re-key %s? The old repo becomes %s/%s", slug, owner, oldName)
		if !prompt.New(o.In, o.Out).Confirm(q, false) {
			o.outf("athome secrets rekey: cancelled, nothing changed\n")
			return exitOK
		}
	}

	if r, err := o.git(ctx, "pull", "-q", "--ff-only"); err != nil || r.Code != 0 {
		o.errf("git pull --ff-only failed; the vault is not up to date\n")
		o.gitFailed(err, r)
		return exitError
	}

	rk := o.Vault + ".rekey"
	if code := o.buildRekey(ctx, rk); code != exitOK {
		return code
	}

	keyFile := filepath.Join(o.Home, ".local", "state", "athome", "rekey-"+date+".key")
	newKey, code := o.exportKey(ctx, rk, keyFile)
	if code != exitOK {
		return code
	}
	keyNote := fmt.Sprintf("the new key is exported at %s; keep it until the vault is sorted out\n", keyFile)

	oldKey, err := o.runOut(ctx, "rbw", "get", "git-crypt/"+o.Name+"-key")
	if err == nil {
		err = keys.Store(ctx, o.Run, oldItem, strings.TrimSpace(oldKey), "")
	}
	if err != nil {
		o.errf("could not store the old key in Bitwarden: %v\n%s", err, keyNote)
		return exitError
	}

	if err := o.gh(ctx, "repo", "rename", oldName, "--repo", slug, "--yes"); err != nil {
		o.errf("%v\n%s", err, keyNote)
		return exitError
	}

	if err := o.gh(ctx, "repo", "create", slug, "--private", "--source", rk, "--remote", "origin", "--push"); err != nil {
		o.errf("%v\nif %s was created (a failed push can create it), delete it with: gh repo delete %s\n"+
			"then undo with: gh repo rename %s --repo %s/%s --yes\n%s",
			err, slug, slug, o.Name, owner, oldName, keyNote)
		return exitError
	}
	if err := keys.Store(ctx, o.Run, "git-crypt/"+o.Name+"-key", base64.StdEncoding.EncodeToString(newKey), ""); err != nil {
		o.errf("could not store the new key in Bitwarden: %v\n"+
			"the new repo %s is live with the new key, exported at %s\n"+
			"retry with: rbw edit git-crypt/%s-key, pasting the base64 of that file (base64 < that file)\n"+
			"the old key is preserved as %s\n",
			err, slug, keyFile, o.Name, oldItem)
		return exitError
	}
	os.Remove(keyFile)

	// GitHub and Bitwarden are correct from here, so a failure names no GitHub undo.
	oldDir := o.Vault + ".old-" + date
	for i := 1; ; i++ {
		if _, err := os.Lstat(oldDir); err != nil {
			break
		}
		oldDir = fmt.Sprintf("%s.old-%s.%d", o.Vault, date, i)
	}
	if err := os.Rename(o.Vault, oldDir); err != nil {
		o.errf("could not move the old vault %s aside: %v\nthe old checkout is still at %s; the new vault is at %s\n", o.Vault, err, o.Vault, rk)
		return exitError
	}
	if err := os.Rename(rk, o.Vault); err != nil {
		o.errf("could not move %s to %s: %v\nthe old checkout is at %s; the new vault is at %s\n", rk, o.Vault, err, oldDir, rk)
		return exitError
	}
	if err := os.RemoveAll(oldDir); err != nil {
		o.errf("could not remove the old checkout %s: %v\nthe new vault is in place at %s; delete %s yourself\n", oldDir, err, o.Vault, oldDir)
		return exitError
	}

	o.outf("✓ vault re-keyed\n")
	o.outf("once you have checked the new vault, run:\n")
	o.outf("  gh repo delete %s/%s\n", owner, oldName)
	o.outf("  rbw remove git-crypt/%s-old-%s-key\n", o.Name, date)
	return exitOK
}

// preconditions checks everything that must hold before anything changes.
func (o RekeyOpts) preconditions(ctx context.Context) int {
	if fi, err := os.Stat(filepath.Join(o.Vault, ".git")); err != nil || !fi.IsDir() {
		o.errf("vault not found at %s; run 'athome secrets restore' to clone it first\n", o.Vault)
		return exitError
	}
	for _, tool := range []string{"git-crypt", "rbw"} {
		if _, err := o.Run.LookPath(tool); err != nil {
			o.errf("%s not found on PATH\n", tool)
			return exitError
		}
	}
	if r, err := o.Run.Run(ctx, run.Cmd{Name: "rbw", Args: []string{"unlocked"}}); err != nil || r.Code != 0 {
		o.errf("rbw is locked; run: rbw unlock, then re-run athome secrets rekey\n")
		return exitError
	}
	r, err := o.git(ctx, "ls-files", "-z")
	if err != nil || r.Code != 0 {
		o.gitFailed(err, r)
		return exitError
	}
	if sentinelEncrypted(o.Vault, nulSplit(r.Stdout)) {
		o.errf("the vault is locked; run git-crypt unlock, then re-run athome secrets rekey\n")
		return exitError
	}
	r, err = o.git(ctx, "status", "--porcelain")
	if err != nil || r.Code != 0 {
		o.gitFailed(err, r)
		return exitError
	}
	if strings.TrimSpace(r.Stdout) != "" {
		o.errf("the vault has uncommitted changes; run athome secrets backup first\n")
		return exitError
	}
	return exitOK
}

// clashes refuses a same-day rerun: the old repo's name or its Bitwarden item
// already existing would be overwritten or fail half way.
func (o RekeyOpts) clashes(ctx context.Context, owner, oldName, oldItem string) int {
	r, err := o.Run.Run(ctx, run.Cmd{Name: "rbw", Args: []string{"list"}})
	if err != nil || r.Code != 0 {
		o.errf("rbw list failed; cannot check for an existing %s\n", oldItem)
		return exitError
	}
	for _, l := range strings.Split(r.Stdout, "\n") {
		if strings.TrimSpace(l) == oldItem {
			o.errf("Bitwarden already has %s; delete it (rbw remove %s) or retry tomorrow\n", oldItem, oldItem)
			return exitError
		}
	}
	r, err = o.Run.Run(ctx, run.Cmd{Name: "gh", Args: []string{"repo", "view", owner + "/" + oldName}})
	if err != nil {
		o.errf("could not check for an existing %s/%s: %v\n", owner, oldName, err)
		return exitError
	}
	if r.Code == 0 {
		o.errf("%s/%s already exists; delete it (gh repo delete %s/%s) or retry tomorrow\n", owner, oldName, owner, oldName)
		return exitError
	}
	return exitOK
}

// owner finds the vault's GitHub owner as restore does.
func (o RekeyOpts) owner(ctx context.Context) string {
	if o.Repo != "" {
		if owner, _, ok := strings.Cut(o.Repo, "/"); ok {
			return owner
		}
	}
	if o.Owner != "" {
		return o.Owner
	}
	if r, err := o.Run.Run(ctx, run.Cmd{Name: "gh", Args: []string{"api", "user", "-q", ".login"}}); err == nil && r.Code == 0 {
		return strings.TrimSpace(r.Stdout)
	}
	return ""
}

// buildRekey makes rk a fresh repo with a new git-crypt key holding every
// tracked file of the vault, committed and gated against plaintext.
func (o RekeyOpts) buildRekey(ctx context.Context, rk string) int {
	if err := os.RemoveAll(rk); err != nil {
		o.errf("%v\n", err)
		return exitError
	}
	if err := os.MkdirAll(rk, 0o755); err != nil {
		o.errf("%v\n", err)
		return exitError
	}
	n := o.Opts
	n.Vault = rk
	for _, c := range []struct {
		name string
		args []string
	}{
		{"git", []string{"init", "-q", "-b", "main"}},
		{"git-crypt", []string{"init"}},
	} {
		if r, err := o.Run.Run(ctx, run.Cmd{Name: c.name, Args: c.args, Dir: rk}); err != nil || r.Code != 0 {
			o.errf("%s %s failed\n", c.name, c.args[0])
			n.gitFailed(err, r)
			return exitError
		}
	}

	r, err := o.git(ctx, "ls-files", "-z")
	if err != nil || r.Code != 0 {
		o.gitFailed(err, r)
		return exitError
	}
	files := append([]string{".gitattributes"}, nulSplit(r.Stdout)...)
	for _, f := range files {
		fi, err := os.Stat(filepath.Join(o.Vault, f))
		if err != nil {
			if f == ".gitattributes" {
				continue
			}
			o.errf("%v\n", err)
			return exitError
		}
		if err := copyFile(filepath.Join(o.Vault, f), filepath.Join(rk, f), fi.Mode().Perm()); err != nil {
			o.errf("could not copy %s: %v\n", f, err)
			return exitError
		}
	}

	if r, err := n.git(ctx, "add", "-A"); err != nil || r.Code != 0 {
		n.gitFailed(err, r)
		return exitError
	}
	if code := n.plaintextGate(ctx); code != exitOK {
		return code
	}
	if r, err := n.git(ctx, "-c", "user.email=secrets@local", "-c", "user.name=secrets-backup", "-c", "commit.gpgsign=false", "commit", "-qm", "rekey: new git-crypt key"); err != nil || r.Code != 0 {
		o.errf("commit failed\n%s", r.Stderr)
		return exitError
	}
	return exitOK
}

// exportKey writes the new repo's git-crypt key to keyFile (0600) and returns
// its bytes.
func (o RekeyOpts) exportKey(ctx context.Context, rk, keyFile string) ([]byte, int) {
	if err := os.MkdirAll(filepath.Dir(keyFile), 0o700); err != nil {
		o.errf("%v\n", err)
		return nil, exitError
	}
	os.Remove(keyFile)
	r, err := o.Run.Run(ctx, run.Cmd{Name: "git-crypt", Args: []string{"export-key", keyFile}, Dir: rk})
	if err != nil || r.Code != 0 {
		o.errf("git-crypt export-key failed\n%s", r.Stderr)
		return nil, exitError
	}
	if err := os.Chmod(keyFile, 0o600); err != nil {
		o.errf("%v\n", err)
		return nil, exitError
	}
	key, err := os.ReadFile(keyFile)
	if err != nil || len(key) == 0 {
		o.errf("git-crypt export-key wrote no key to %s\n", keyFile)
		return nil, exitError
	}
	return key, exitOK
}

// runOut runs a command and returns its stdout, failing on a non-zero exit.
func (o RekeyOpts) runOut(ctx context.Context, name string, args ...string) (string, error) {
	r, err := o.Run.Run(ctx, run.Cmd{Name: name, Args: args})
	if err != nil {
		return "", err
	}
	if r.Code != 0 {
		return "", fmt.Errorf("%s %s failed: %s", name, strings.Join(args, " "), strings.TrimSpace(r.Stderr))
	}
	return r.Stdout, nil
}

func (o RekeyOpts) gh(ctx context.Context, args ...string) error {
	_, err := o.runOut(ctx, "gh", args...)
	return err
}
