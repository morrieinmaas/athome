package keys

import (
	"context"
	"fmt"
	"strings"

	"github.com/morrieinmaas/athome/internal/run"
)

const scopeHint = "gh auth refresh -h github.com -s admin:public_key,admin:ssh_signing_key"

// ghAdd registers pub on GitHub as an authentication and a signing key.
func ghAdd(ctx context.Context, r run.Runner, pub, host string) error {
	for _, args := range [][]string{
		{"ssh-key", "add", pub, "--title", host},
		{"ssh-key", "add", pub, "--title", host + "-sign", "--type", "signing"},
	} {
		res, err := r.Run(ctx, run.Cmd{Name: "gh", Args: args})
		if err != nil {
			return err
		}
		// A retry after a half-finished add finds the key there already.
		if res.Code != 0 && !strings.Contains(res.Stderr, "already") {
			return fmt.Errorf("gh %s failed: %s (missing scopes? run: %s)", strings.Join(args, " "), strings.TrimSpace(res.Stderr), scopeHint)
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
	blob := pubBlob(pub)
	if blob == "" {
		return nil
	}
	for _, endpoint := range []string{"user/keys", "user/ssh_signing_keys"} {
		res, err := r.Run(ctx, run.Cmd{Name: "gh", Args: []string{"api", "--paginate", endpoint, "--jq", `.[] | "\(.id) \(.key)"`}})
		if err != nil {
			return err
		}
		if res.Code != 0 {
			return fmt.Errorf("gh api %s failed: %s", endpoint, strings.TrimSpace(res.Stderr))
		}
		for _, line := range strings.Split(res.Stdout, "\n") {
			f := strings.Fields(line)
			if len(f) < 3 || f[2] != blob {
				continue
			}
			del, err := r.Run(ctx, run.Cmd{Name: "gh", Args: []string{"api", "-X", "DELETE", endpoint + "/" + f[0]}})
			if err != nil {
				return err
			}
			if del.Code != 0 {
				return fmt.Errorf("gh api -X DELETE %s/%s failed: %s", endpoint, f[0], strings.TrimSpace(del.Stderr))
			}
		}
	}
	return nil
}

// pubBlob is the base64 part of an ssh public key line.
func pubBlob(pub string) string {
	f := strings.Fields(pub)
	if len(f) < 2 {
		return ""
	}
	return f[1]
}
