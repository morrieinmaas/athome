// Package secrets backs the local secret files up to an encrypted git vault
// and restores them on a new machine. It is the port of the secrets-backup and
// secrets-restore scripts; every external tool goes through run.Runner.
package secrets

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/morrieinmaas/athome/internal/run"
)

const (
	exitOK    = 0
	exitError = 1
	exitAbort = 2
)

// Opts is everything Backup and Restore need. Repo is the vault's GitHub
// repo; when empty, Restore builds it from Owner (or the logged-in gh user)
// and Name.
type Opts struct {
	Home   string
	Vault  string
	Name   string
	Owner  string
	Repo   string
	DryRun bool
	Run    run.Runner
	Out    io.Writer
	Err    io.Writer
}

// FromEnv builds the options from the environment the scripts read:
// SECRETS_VAULT, SECRETS_VAULT_NAME, SECRETS_VAULT_REPO, ATHOME_GITHUB_HANDLE.
func FromEnv(home string, r run.Runner, out, errw io.Writer) Opts {
	name := envOr("SECRETS_VAULT_NAME", "attic")
	return Opts{
		Home:  home,
		Vault: envOr("SECRETS_VAULT", filepath.Join(home, ".local", "share", name)),
		Name:  name,
		Owner: os.Getenv("ATHOME_GITHUB_HANDLE"),
		Repo:  os.Getenv("SECRETS_VAULT_REPO"),
		Run:   r,
		Out:   out,
		Err:   errw,
	}
}

// BackupFromEnv is FromEnv for backup: the default vault stays ~/.local/share/attic
// whatever SECRETS_VAULT_NAME says, as the secrets-backup script had it.
func BackupFromEnv(home string, r run.Runner, out, errw io.Writer) Opts {
	o := FromEnv(home, r, out, errw)
	o.Vault = envOr("SECRETS_VAULT", filepath.Join(home, ".local", "share", "attic"))
	return o
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func (o Opts) outf(format string, a ...any) {
	if o.Out != nil {
		fmt.Fprintf(o.Out, format, a...)
	}
}

func (o Opts) errf(format string, a ...any) {
	if o.Err != nil {
		fmt.Fprintf(o.Err, format, a...)
	}
}

func (o Opts) git(ctx context.Context, args ...string) (run.Result, error) {
	return o.Run.Run(ctx, run.Cmd{Name: "git", Args: args, Dir: o.Vault})
}

// gitFailed reports a failed git call: the run error, else git's stderr.
func (o Opts) gitFailed(err error, r run.Result) {
	if err != nil {
		o.errf("%v\n", err)
		return
	}
	o.errf("git failed\n%s", r.Stderr)
}

// cryptHeader reports whether data starts with the git-crypt header: the
// first nine bytes, NULs dropped, read GITCRYPT.
func cryptHeader(data []byte) bool {
	if len(data) > 9 {
		data = data[:9]
	}
	return string(bytes.ReplaceAll(data, []byte{0}, nil)) == "GITCRYPT"
}

func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// nulSplit splits git's -z output, which names files byte for byte instead of quoting them.
func nulSplit(s string) []string {
	var out []string
	for _, f := range strings.Split(s, "\x00") {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

func mirrorSources(home string) []string {
	var out []string
	for _, pat := range []string{".env", ".env.*"} {
		m, _ := filepath.Glob(filepath.Join(globEscape(home), ".secrets", "*", pat))
		for _, f := range m {
			// bash's * does not match a leading dot
			if strings.HasPrefix(filepath.Base(filepath.Dir(f)), ".") {
				continue
			}
			switch filepath.Ext(f) {
			case ".example", ".sample", ".template", ".dist":
				continue
			}
			out = append(out, f)
		}
	}
	for _, rel := range []string{
		".aws/config", ".aws/credentials",
		".config/scw/config.yaml", ".config/stripe/config.toml",
		".codeium/windsurf/mcp_config.json",
	} {
		out = append(out, filepath.Join(home, rel))
	}
	return out
}

func globEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`, `[`, `\[`).Replace(s)
}

func copyFile(src, dst string, perm os.FileMode) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, b, perm)
}

// Backup mirrors the secret files into the vault, commits and pushes. It
// returns the process exit code.
func Backup(ctx context.Context, o Opts) int {
	unlock, ok := o.acquire()
	if !ok {
		return exitError
	}
	defer unlock()
	if fi, err := os.Stat(filepath.Join(o.Vault, ".git")); err != nil || !fi.IsDir() {
		o.errf("vault not found at %s; run 'athome secrets restore' to clone it first\n", o.Vault)
		return exitError
	}
	o.mirror()

	if a, err := o.git(ctx, "add", "-A"); err != nil || a.Code != 0 {
		o.gitFailed(err, a)
		return exitError
	}
	r, err := o.git(ctx, "diff", "--cached", "--quiet")
	if err != nil {
		o.errf("%v\n", err)
		return exitError
	}
	if r.Code == 0 {
		o.outf("✓ vault already in sync (no changes)\n")
		return exitOK
	}
	if code := o.plaintextGate(ctx); code != exitOK {
		return code
	}
	return o.commitPush(ctx)
}

// mirror copies the secret files that exist under HOME into the vault.
func (o Opts) mirror() {
	for _, src := range mirrorSources(o.Home) {
		fi, err := os.Stat(src)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		rel, err := filepath.Rel(o.Home, src)
		if err != nil {
			continue
		}
		if err := copyFile(src, filepath.Join(o.Vault, rel), fi.Mode().Perm()); err != nil {
			o.errf("could not mirror %s: %v\n", src, err)
		}
	}
}

// plaintextGate is the safety gate: every staged file must be ciphertext in
// the index. -z keeps non-ASCII names unquoted.
func (o Opts) plaintextGate(ctx context.Context) int {
	r, err := o.git(ctx, "diff", "--cached", "--name-only", "-z")
	if err != nil || r.Code != 0 {
		o.gitFailed(err, r)
		return exitError
	}
	bad := false
	for _, f := range nulSplit(r.Stdout) {
		if f == ".gitattributes" {
			continue
		}
		c, err := o.git(ctx, "cat-file", "-p", ":"+f)
		if err != nil || !cryptHeader([]byte(c.Stdout)) {
			o.outf("  !! NOT ENCRYPTED in index: %s\n", f)
			bad = true
		}
	}
	if bad {
		o.errf("ABORT: plaintext detected in staged files; refusing to commit/push. Is git-crypt unlocked & .gitattributes intact?\n")
		return exitAbort
	}
	return exitOK
}

// commitPush commits the staged snapshot and pushes it. A failed commit exits 1
// (the script carried on); a failed push only warns.
func (o Opts) commitPush(ctx context.Context) int {
	msg := "secrets snapshot " + time.Now().UTC().Format("2006-01-02T15:04:05Z")
	if c, err := o.git(ctx, "-c", "user.email=secrets@local", "-c", "user.name=secrets-backup", "commit", "-qm", msg); err != nil || c.Code != 0 {
		o.errf("commit failed\n%s", c.Stderr)
		return exitError
	}
	sha := ""
	if s, err := o.git(ctx, "rev-parse", "--short", "HEAD"); err == nil {
		sha = strings.TrimSpace(s.Stdout)
	}
	if p, err := o.git(ctx, "push", "-q", "origin", "HEAD"); err == nil && p.Code == 0 {
		n := 0
		if d, err := o.git(ctx, "diff", "--name-only", "HEAD~1"); err == nil {
			n = len(lines(d.Stdout))
		}
		o.outf("✓ backed up + pushed (%s), %d file(s) changed\n", sha, n)
	} else {
		o.outf("⚠ committed locally (%s) but push failed (offline / no auth); will push next run\n", sha)
	}
	return exitOK
}

// Restore clones or pulls the vault, unlocks it with the key from Bitwarden
// when it is still encrypted and copies the files into place.
func Restore(ctx context.Context, o Opts) int {
	if fi, err := os.Stat(filepath.Join(o.Vault, ".git")); err == nil && fi.IsDir() {
		o.git(ctx, "pull", "-q", "--ff-only") // a failed pull is fine: offline restore of what is there
	} else if code := o.clone(ctx); code != exitOK {
		return code
	}

	r, err := o.git(ctx, "ls-files", "-z")
	if err != nil {
		o.errf("%v\n", err)
		return exitError
	}
	if r.Code != 0 {
		o.errf("git ls-files failed in %s\n%s", o.Vault, r.Stderr)
		return exitError
	}
	files := nulSplit(r.Stdout)

	if sentinelEncrypted(o.Vault, files) {
		if code := o.unlock(ctx); code != exitOK {
			return code
		}
	}

	rc := o.copyTracked(files)
	if !o.DryRun {
		hardenSecretsTree(filepath.Join(o.Home, ".secrets"))
	}
	return rc
}

// copyTracked copies the vault's files into Home at mode 600, or only lists
// them with DryRun. It returns exitError if any file failed.
func (o Opts) copyTracked(files []string) int {
	rc := exitOK
	for _, f := range files {
		if f == ".gitattributes" {
			continue
		}
		target := filepath.Join(o.Home, f)
		if o.DryRun {
			o.outf("  would restore  %s\n", target)
			continue
		}
		err := copyFile(filepath.Join(o.Vault, f), target, 0o600)
		if err == nil {
			err = os.Chmod(target, 0o600)
		}
		if err != nil {
			o.outf("  FAIL  %s\n", f)
			rc = exitError
			continue
		}
		o.outf("  restored  %s\n", target)
	}
	return rc
}

func (o Opts) clone(ctx context.Context) int {
	repo := o.Repo
	if repo == "" {
		owner := o.Owner
		if owner == "" {
			if r, err := o.Run.Run(ctx, run.Cmd{Name: "gh", Args: []string{"api", "user", "-q", ".login"}}); err == nil && r.Code == 0 {
				owner = strings.TrimSpace(r.Stdout)
			}
		}
		if owner == "" {
			o.errf("cannot work out the vault owner; set ATHOME_GITHUB_HANDLE or SECRETS_VAULT_REPO, or log in with `gh auth login`\n")
			return exitError
		}
		repo = owner + "/" + o.Name
	}
	o.outf("cloning %s to %s\n", repo, o.Vault)
	if err := os.MkdirAll(filepath.Dir(o.Vault), 0o755); err != nil {
		o.errf("%v\n", err)
		return exitError
	}
	r, err := o.Run.Run(ctx, run.Cmd{Name: "gh", Args: []string{"repo", "clone", repo, o.Vault, "--", "-q"}, Interactive: true})
	if err != nil || r.Code != 0 {
		o.errf("could not clone %s into %s\n", repo, o.Vault)
		return exitError
	}
	return exitOK
}

// sentinelEncrypted: the first tracked file other than .gitattributes is
// still ciphertext.
func sentinelEncrypted(vault string, files []string) bool {
	for _, f := range files {
		if f == ".gitattributes" {
			continue
		}
		fh, err := os.Open(filepath.Join(vault, f))
		if err != nil {
			return false
		}
		defer fh.Close()
		buf := make([]byte, 9)
		n, _ := io.ReadFull(fh, buf)
		return cryptHeader(buf[:n])
	}
	return false
}

func (o Opts) unlock(ctx context.Context) int {
	locked := "rbw is locked; run: rbw unlock, then re-run athome secrets restore\n"
	if _, err := o.Run.LookPath("rbw"); err != nil {
		o.errf("%s", locked)
		return exitError
	}
	if r, err := o.Run.Run(ctx, run.Cmd{Name: "rbw", Args: []string{"unlocked"}}); err != nil || r.Code != 0 {
		o.errf("%s", locked)
		return exitError
	}
	r, err := o.Run.Run(ctx, run.Cmd{Name: "rbw", Args: []string{"get", "git-crypt/" + o.Name + "-key"}})
	if err != nil || r.Code != 0 {
		o.errf("could not read git-crypt/%s-key from Bitwarden\n", o.Name)
		return exitError
	}
	// The item's password is the base64 of the key.
	key, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(r.Stdout, "\n", ""))
	if err != nil {
		o.errf("git-crypt/%s-key is not valid base64: %v\n", o.Name, err)
		return exitError
	}
	f, err := os.CreateTemp("", "athome-gitcrypt-key-*")
	if err != nil {
		o.errf("%v\n", err)
		return exitError
	}
	defer os.Remove(f.Name())
	_, werr := f.Write(key)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		o.errf("%v\n", werr)
		return exitError
	}
	u, err := o.Run.Run(ctx, run.Cmd{Name: "git-crypt", Args: []string{"unlock", f.Name()}, Dir: o.Vault})
	if err != nil || u.Code != 0 {
		o.errf("git-crypt unlock failed\n%s", u.Stderr)
		return exitError
	}
	o.outf("unlocked vault with key from Bitwarden\n")
	return exitOK
}

// hardenSecretsTree makes ~/.secrets and its directories owner-only.
func hardenSecretsTree(root string) {
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return
	}
	os.Chmod(root, 0o700)
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() && p != root {
			os.Chmod(p, 0o700)
		}
		return nil
	})
}
