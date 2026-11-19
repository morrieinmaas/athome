package keys

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/morrieinmaas/athome/internal/run"
)

// ScopeHint is the gh command that grants the key scopes gh ssh-key add needs.
const ScopeHint = "gh auth refresh -h github.com -s admin:public_key,admin:ssh_signing_key"

// ghAdd registers pub on GitHub as an authentication and a signing key.
func ghAdd(ctx context.Context, r run.Runner, pub, host string) error {
	already := false
	for _, args := range [][]string{
		{"ssh-key", "add", pub, "--title", host},
		{"ssh-key", "add", pub, "--title", host + "-sign", "--type", "signing"},
	} {
		res, err := r.Run(ctx, run.Cmd{Name: "gh", Args: args})
		if err != nil {
			return err
		}
		if res.Code != 0 {
			// A retry after a half-finished add finds the key there already,
			// but GitHub says the same when the key is on a different account.
			if !strings.Contains(res.Stderr, "already") {
				return fmt.Errorf("gh %s failed: %s (missing scopes? run: %s)", strings.Join(args, " "), strings.TrimSpace(res.Stderr), ScopeHint)
			}
			already = true
		}
	}
	if !already {
		return nil
	}
	return ghCheckAdded(ctx, r, pub)
}

// ghCheckAdded fails unless the key in file pub is on the account gh acts as,
// as both an authentication and a signing key.
func ghCheckAdded(ctx context.Context, r run.Runner, pub string) error {
	data, err := os.ReadFile(pub)
	if err != nil {
		return err
	}
	blob := PubBlob(string(data))
	for _, endpoint := range GHKeyEndpoints {
		ks, err := GHKeys(ctx, r, endpoint, false)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(ks, func(k GHKey) bool { return k.Blob == blob }) {
			return fmt.Errorf("GitHub said the key is already there, but %s lists no such key: it may be on another account", endpoint)
		}
	}
	return nil
}

// ghLogin proves that key alone authenticates to GitHub. GitHub's success
// banner comes with exit 1, so the output decides.
func ghLogin(ctx context.Context, r run.Runner, key string) error {
	res, err := r.Run(ctx, run.Cmd{Name: "ssh", Args: []string{
		"-F", "/dev/null", "-i", key, "-o", "IdentitiesOnly=yes", "-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new", "-T", "git@github.com",
	}})
	if err != nil {
		return err
	}
	if !strings.Contains(res.Stdout+res.Stderr, "successfully authenticated") {
		return fmt.Errorf("the new key does not log in to GitHub: %s", strings.TrimSpace(res.Stderr))
	}
	return nil
}

// ghRemove deletes every GitHub authentication and signing key whose blob is
// the one in pub. A key already gone is not an error.
func ghRemove(ctx context.Context, r run.Runner, pub string) error {
	return ghRemoveTitled(ctx, r, pub, nil)
}

// ghRemoveTitled is ghRemove limited, when titles is set, to keys whose title
// is titles[endpoint]; the listing then carries the title too.
func ghRemoveTitled(ctx context.Context, r run.Runner, pub string, titles map[string]string) error {
	blob := PubBlob(pub)
	if blob == "" {
		return nil
	}
	for _, endpoint := range GHKeyEndpoints {
		ks, err := GHKeys(ctx, r, endpoint, titles != nil)
		if err != nil {
			return err
		}
		for _, k := range ks {
			if k.Blob != blob || (titles != nil && k.Title != titles[endpoint]) {
				continue
			}
			if err := GHDeleteKey(ctx, r, endpoint, k.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// GHKey is one key a GitHub account lists at one of GHKeyEndpoints.
type GHKey struct{ ID, Blob, Title string }

// GHKeyEndpoints are the account's authentication and signing key listings.
var GHKeyEndpoints = []string{"user/keys", "user/ssh_signing_keys"}

// GHKeys lists every page of the keys at endpoint. withTitle asks for each
// key's title too; without it Title is empty.
func GHKeys(ctx context.Context, r run.Runner, endpoint string, withTitle bool) ([]GHKey, error) {
	jq := `.[] | "\(.id) \(.key)"`
	if withTitle {
		jq = `.[] | "\(.id) \(.key) \(.title)"`
	}
	res, err := r.Run(ctx, run.Cmd{Name: "gh", Args: []string{"api", "--paginate", endpoint, "--jq", jq}})
	if err != nil {
		return nil, err
	}
	if res.Code != 0 {
		return nil, fmt.Errorf("gh api %s failed: %s", endpoint, strings.TrimSpace(res.Stderr))
	}
	var out []GHKey
	for _, line := range strings.Split(res.Stdout, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		out = append(out, GHKey{ID: f[0], Blob: f[2], Title: strings.Join(f[3:], " ")})
	}
	return out, nil
}

// GHDeleteKey deletes the key with id at endpoint.
func GHDeleteKey(ctx context.Context, r run.Runner, endpoint, id string) error {
	res, err := r.Run(ctx, run.Cmd{Name: "gh", Args: []string{"api", "-X", "DELETE", endpoint + "/" + id}})
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("gh api -X DELETE %s/%s failed: %s", endpoint, id, strings.TrimSpace(res.Stderr))
	}
	return nil
}

// WithPubFile writes pub to a temp file, the form gh ssh-key add takes, calls
// fn with its path and removes the file again.
func WithPubFile(pub string, fn func(path string) error) error {
	f, err := os.CreateTemp("", "athome-box-*.pub")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(pub + "\n"); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return fn(f.Name())
}

// ghEnvRunner adds env to every command, so an account's token reaches gh
// without ever being an argument.
type ghEnvRunner struct {
	run.Runner
	env []string
}

func (r ghEnvRunner) Run(ctx context.Context, c run.Cmd) (run.Result, error) {
	c.Env = append(append([]string(nil), c.Env...), r.env...)
	return r.Runner.Run(ctx, c)
}

// GHTokenRunner is r when account is "", else r with GH_TOKEN of that gh
// account on every command, in the environment only.
func GHTokenRunner(ctx context.Context, r run.Runner, account string) (run.Runner, error) {
	if account == "" {
		return r, nil
	}
	res, err := r.Run(ctx, run.Cmd{Name: "gh", Args: []string{"auth", "token", "--hostname", "github.com", "--user", account}})
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(res.Stdout)
	if res.Code != 0 || token == "" {
		return nil, &NoTokenError{Account: account, Stderr: strings.TrimSpace(res.Stderr)}
	}
	return ghEnvRunner{r, []string{"GH_TOKEN=" + token}}, nil
}

// NoTokenError is GHTokenRunner's error when gh has no token for Account;
// Stderr is what gh auth token said.
type NoTokenError struct{ Account, Stderr string }

func (e *NoTokenError) Error() string {
	return fmt.Sprintf("gh has no token for GitHub account %s (run: gh auth login)", e.Account)
}

// PubBlob is the base64 part of an ssh public key line.
func PubBlob(pub string) string {
	f := strings.Fields(pub)
	if len(f) < 2 {
		return ""
	}
	return f[1]
}
