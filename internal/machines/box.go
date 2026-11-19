package machines

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/morrieinmaas/athome/internal/keys"
	"github.com/morrieinmaas/athome/internal/run"
)

// Box is an inventory machine reached over ssh. It implements keys.Box and the
// box-key rotation methods of keys.KeyBox.
type Box struct {
	r Remote
	m Machine
}

func NewBox(r Remote, m Machine) *Box { return &Box{r: r, m: m} }

// KeysInventory returns one Box per inventory machine. With any machine it
// fails unless ~/.ssh/config includes the generated config, which it rewrites
// first, so a rotation never reaches a box through whatever else the ssh config
// says or through a bare name outside its pinned Host block.
func KeysInventory(home string, r run.Runner, host string) ([]keys.Box, error) {
	ms, err := Load(home)
	if err != nil {
		return nil, err
	}
	if len(ms) > 0 {
		if err := CheckInclude(home); err != nil {
			return nil, err
		}
		if err := Refresh(home, filepath.Base(keys.LaptopKey(home, host))); err != nil {
			return nil, err
		}
	}
	out := make([]keys.Box, 0, len(ms))
	for _, m := range ms {
		out = append(out, NewBox(Remote{Run: r, Home: home, Host: host}, m))
	}
	return out, nil
}

func (b *Box) Name() string          { return b.m.Name }
func (b *Box) Scope() string         { return b.m.Scope }
func (b *Box) GitHubAccount() string { return b.m.GitHubAccount }

// pubLineRe is one ssh-ed25519 line: no option prefix, no newline.
var pubLineRe = regexp.MustCompile(`^ssh-ed25519 [A-Za-z0-9+/]+={0,2}( [^\x00-\x1f\x7f]*)?$`)

func checkPub(pub string) error {
	if !pubLineRe.MatchString(pub) {
		return errors.New("not a single-line ssh-ed25519 public key")
	}
	return nil
}

// sh runs script on the box; a non-zero exit is an error naming the box.
func (b *Box) sh(ctx context.Context, what, script, stdin string, args ...string) (run.Result, error) {
	res, err := b.r.Exec(ctx, b.m.Name, script, stdin, args...)
	if err != nil {
		return res, fmt.Errorf("%s on %s: %w", what, b.m.Name, err)
	}
	if res.Code != 0 {
		return res, fmt.Errorf("%s on %s failed: %s", what, b.m.Name, strings.TrimSpace(res.Stderr))
	}
	return res, nil
}

const prepareSSH = `umask 077; mkdir -p "$HOME/.ssh" && chmod 700 "$HOME/.ssh" || exit 1; `

const authorizeScript = prepareSSH + `f="$HOME/.ssh/authorized_keys"
touch "$f" && chmod 600 "$f" || exit 1
k=$(cat)
b=$(printf '%s\n' "$k" | awk '{print $2}')
if awk -v b="$b" '/^[[:space:]]*#/ {next} {for (i = 1; i <= NF; i++) if ($i == b) found = 1} END {exit !found}' "$f"; then exit 0; fi
if [ -s "$f" ] && [ -n "$(tail -c1 "$f")" ]; then echo >> "$f"; fi
printf '%s\n' "$k" >> "$f"`

func (b *Box) Authorize(ctx context.Context, pub string) error {
	if err := checkPub(pub); err != nil {
		return fmt.Errorf("authorize on %s: %w", b.m.Name, err)
	}
	_, err := b.sh(ctx, "authorize", authorizeScript, pub+"\n")
	return err
}

const resetScript = prepareSSH + `f="$HOME/.ssh/authorized_keys"
n="$HOME/.ssh/authorized_keys.athome"
cat > "$n" && chmod 600 "$n" && mv "$n" "$f"`

func (b *Box) Reset(ctx context.Context, managed []string) error {
	if len(managed) == 0 {
		return fmt.Errorf("reset on %s: no keys to manage", b.m.Name)
	}
	var in strings.Builder
	for _, k := range managed {
		if err := checkPub(k); err != nil {
			return fmt.Errorf("reset on %s: %w", b.m.Name, err)
		}
		in.WriteString(k + "\n")
	}
	_, err := b.sh(ctx, "reset", resetScript, in.String())
	return err
}

func (b *Box) Login(ctx context.Context, keyPath string) error {
	return b.r.LoginCheck(ctx, b.m, keyPath)
}

func (b *Box) needHostname() error {
	if b.m.Hostname == "" {
		return fmt.Errorf("machine %s has no hostname recorded; rerun athome machine add %s", b.m.Name, b.m.Name)
	}
	return nil
}

// CurrentPub is the box key's public key.
func (b *Box) CurrentPub(ctx context.Context) (string, error) {
	if err := b.needHostname(); err != nil {
		return "", err
	}
	res, err := b.sh(ctx, "read key", `cat "$HOME/.ssh/$1_ed25519.pub"`, "", b.m.Hostname)
	return strings.TrimSpace(res.Stdout), err
}

// keyScript creates ~/.ssh/<$1>_ed25519<suffix> on the box unless it exists,
// comment athome@<$2>, and prints its public key.
func keyScript(suffix string) string {
	return prepareSSH + `k="$HOME/.ssh/$1_ed25519` + suffix + `"
[ -f "$k" ] || ssh-keygen -q -t ed25519 -N '' -C "athome@$2" -f "$k" || exit 1
[ -f "$k.pub" ] || ssh-keygen -y -f "$k" > "$k.pub" || exit 1
cat "$k.pub"`
}

// boxKeyScript is machine add's box key; newKeyScript is a rotation's staging key.
var (
	boxKeyScript = keyScript("")
	newKeyScript = keyScript(".rotate")
)

// NewKey creates ~/.ssh/<hostname>_ed25519.rotate unless it exists and returns its public key.
func (b *Box) NewKey(ctx context.Context) (string, error) {
	if err := b.needHostname(); err != nil {
		return "", err
	}
	res, err := b.sh(ctx, "new key", newKeyScript, "", b.m.Hostname, b.m.Name)
	if err != nil {
		return "", err
	}
	pub := strings.TrimSpace(res.Stdout)
	if err := checkPub(pub); err != nil {
		return "", fmt.Errorf("new key on %s: %w", b.m.Name, err)
	}
	return pub, nil
}

// pinGitHubHosts reads GitHub's host keys from stdin into a file of their own,
// so nothing that talks to GitHub from the box trusts on first use.
const pinGitHubHosts = prepareSSH + `kh="$HOME/.ssh/athome_github_known_hosts"
cat > "$kh" && chmod 600 "$kh" || exit 1
`

const checkGitHubScript = pinGitHubHosts + `ssh -F /dev/null -i "$HOME/.ssh/$1_ed25519$2" -o IdentitiesOnly=yes -o BatchMode=yes -o UserKnownHostsFile="$kh" -o StrictHostKeyChecking=yes -T git@github.com`

// CheckGitHub proves from the box that the key (or the .rotate key) logs in to GitHub.
// GitHub's success banner comes with exit 1, so the output decides. With a
// known account the banner must greet it (Hi <account>!): a key that logs in
// as another account is not the box's key.
func (b *Box) CheckGitHub(ctx context.Context, rotating bool) error {
	if err := b.needHostname(); err != nil {
		return err
	}
	suffix := ""
	if rotating {
		suffix = ".rotate"
	}
	res, err := b.r.Exec(ctx, b.m.Name, checkGitHubScript, githubKnownHosts, b.m.Hostname, suffix)
	if err != nil {
		return fmt.Errorf("github check on %s: %w", b.m.Name, err)
	}
	out := res.Stdout + res.Stderr
	if !strings.Contains(out, "successfully authenticated") {
		return fmt.Errorf("the box key on %s does not log in to GitHub: %s", b.m.Name, strings.TrimSpace(res.Stderr))
	}
	if acct := b.m.GitHubAccount; acct != "" && !strings.Contains(strings.ToLower(out), strings.ToLower("Hi "+acct+"!")) {
		return fmt.Errorf("the box key on %s logs in to GitHub as another account than %s: %s", b.m.Name, acct, strings.TrimSpace(res.Stderr))
	}
	return nil
}

// promoteScript moves the public key first: an interruption between the two
// moves leaves .rotate in place, and a rerun finishes the job. A .rotate.pub
// on its own is an unfinished promotion too.
const promoteScript = `k="$HOME/.ssh/$1_ed25519"
if [ -f "$k.rotate" ] || [ -f "$k.rotate.pub" ]; then
  if [ -f "$k.rotate.pub" ]; then mv "$k.rotate.pub" "$k.pub" || exit 1; fi
  if [ -f "$k.rotate" ]; then mv "$k.rotate" "$k" || exit 1; fi
  exit 0
fi
[ "$(awk '{print $2}' "$k.pub")" = "$2" ] || { echo "no .rotate key and the current key is not the new one" >&2; exit 1; }`

// PromoteKey moves the .rotate key over the key. It is a no-op when .rotate
// is gone and the key already is newPub.
func (b *Box) PromoteKey(ctx context.Context, newPub string) error {
	if err := b.needHostname(); err != nil {
		return err
	}
	if err := checkPub(newPub); err != nil {
		return fmt.Errorf("promote on %s: %w", b.m.Name, err)
	}
	_, err := b.sh(ctx, "promote key", promoteScript, "", b.m.Hostname, keys.PubBlob(newPub))
	return err
}
