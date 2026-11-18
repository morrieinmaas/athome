package machines

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/morrieinmaas/athome/internal/run"
)

const scopeHint = "gh auth refresh -h github.com -s admin:public_key,admin:ssh_signing_key"

// ghClient runs gh for one machine. With a github_account every call carries
// GH_TOKEN from that account, in the environment only, never in args or output.
type ghClient struct {
	r       run.Runner
	account string
	token   string
}

func (g *ghClient) run(ctx context.Context, args ...string) (run.Result, error) {
	c := run.Cmd{Name: "gh", Args: args}
	if g.account != "" {
		if g.token == "" {
			res, err := g.r.Run(ctx, run.Cmd{Name: "gh", Args: []string{"auth", "token", "--hostname", "github.com", "--user", g.account}})
			if err != nil {
				return res, err
			}
			g.token = strings.TrimSpace(res.Stdout)
			if res.Code != 0 || g.token == "" {
				return res, fmt.Errorf("no gh token for account %s: %s", g.account, strings.TrimSpace(res.Stderr))
			}
		}
		c.Env = []string{"GH_TOKEN=" + g.token}
	}
	return g.r.Run(ctx, c)
}

type ghKey struct{ ID, Blob, Title string }

var ghEndpoints = []string{"user/keys", "user/ssh_signing_keys"}

// keys lists the account's keys at endpoint (user/keys or user/ssh_signing_keys).
func (g *ghClient) keys(ctx context.Context, endpoint string) ([]ghKey, error) {
	res, err := g.run(ctx, "api", "--paginate", endpoint, "--jq", `.[] | "\(.id) \(.key) \(.title)"`)
	if err != nil {
		return nil, err
	}
	if res.Code != 0 {
		return nil, fmt.Errorf("gh api %s failed: %s", endpoint, strings.TrimSpace(res.Stderr))
	}
	var out []ghKey
	for _, line := range strings.Split(res.Stdout, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		out = append(out, ghKey{ID: f[0], Blob: f[2], Title: strings.Join(f[3:], " ")})
	}
	return out, nil
}

func hasBlob(ks []ghKey, blob string) bool {
	for _, k := range ks {
		if k.Blob == blob {
			return true
		}
	}
	return false
}

// registered reports whether blob is on GitHub as an authentication and as a signing key.
func (g *ghClient) registered(ctx context.Context, blob string) (auth, sign bool, err error) {
	ak, err := g.keys(ctx, ghEndpoints[0])
	if err != nil {
		return false, false, err
	}
	sk, err := g.keys(ctx, ghEndpoints[1])
	if err != nil {
		return false, false, err
	}
	return hasBlob(ak, blob), hasBlob(sk, blob), nil
}

// register adds whichever of the two keys pub is missing as, titled athome-<name>[-sign].
func (g *ghClient) register(ctx context.Context, name, pub string) error {
	auth, sign, err := g.registered(ctx, blobOf(pub))
	if err != nil {
		return err
	}
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
	for _, a := range []struct {
		have  bool
		title string
		extra []string
	}{
		{auth, "athome-" + name, nil},
		{sign, "athome-" + name + "-sign", []string{"--type", "signing"}},
	} {
		if a.have {
			continue
		}
		args := append([]string{"ssh-key", "add", f.Name(), "--title", a.title}, a.extra...)
		res, err := g.run(ctx, args...)
		if err != nil {
			return err
		}
		if res.Code != 0 && !strings.Contains(res.Stderr, "already") {
			return fmt.Errorf("gh ssh-key add --title %s failed: %s (missing scopes? run: %s)", a.title, strings.TrimSpace(res.Stderr), scopeHint)
		}
	}
	return nil
}

// deleteByTitle deletes every authentication and signing key titled exactly
// athome-<name> or athome-<name>-sign.
func (g *ghClient) deleteByTitle(ctx context.Context, name string) error {
	titles := map[string]bool{"athome-" + name: true, "athome-" + name + "-sign": true}
	for _, endpoint := range ghEndpoints {
		ks, err := g.keys(ctx, endpoint)
		if err != nil {
			return err
		}
		for _, k := range ks {
			if !titles[k.Title] {
				continue
			}
			res, err := g.run(ctx, "api", "-X", "DELETE", endpoint+"/"+k.ID)
			if err != nil {
				return err
			}
			if res.Code != 0 {
				return fmt.Errorf("gh api -X DELETE %s/%s failed: %s", endpoint, k.ID, strings.TrimSpace(res.Stderr))
			}
		}
	}
	return nil
}
