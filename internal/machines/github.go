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

// login is the account gh is logged in to, for a machine that records none.
func (g *ghClient) login(ctx context.Context) (string, error) {
	res, err := g.run(ctx, "api", "user", "--jq", ".login")
	if err != nil {
		return "", err
	}
	login := strings.TrimSpace(res.Stdout)
	if res.Code != 0 || login == "" {
		return "", fmt.Errorf("gh api user failed: %s", strings.TrimSpace(res.Stderr))
	}
	return login, nil
}

// deleteByTitle deletes the authentication key titled athome-<name> and the
// signing key titled athome-<name>-sign, and returns how many it deleted. Each
// title is matched on its own endpoint only, so another machine called
// <name>-sign keeps its keys.
func (g *ghClient) deleteByTitle(ctx context.Context, name string) (int, error) {
	n := 0
	for _, e := range []struct{ endpoint, title string }{
		{ghEndpoints[0], "athome-" + name},
		{ghEndpoints[1], "athome-" + name + "-sign"},
	} {
		ks, err := g.keys(ctx, e.endpoint)
		if err != nil {
			return n, err
		}
		for _, k := range ks {
			if k.Title != e.title {
				continue
			}
			res, err := g.run(ctx, "api", "-X", "DELETE", e.endpoint+"/"+k.ID)
			if err != nil {
				return n, err
			}
			if res.Code != 0 {
				return n, fmt.Errorf("gh api -X DELETE %s/%s failed: %s", e.endpoint, k.ID, strings.TrimSpace(res.Stderr))
			}
			n++
		}
	}
	return n, nil
}

// githubKnownHosts is GitHub's published SSH host keys (gh api meta), pinned on
// the box so its check of github.com never trusts on first use.
const githubKnownHosts = `github.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl
github.com ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBEmKSENjQEezOmxkZMy7opKgwFB9nkt5YRrYMjNuG5N87uRgg6CLrbo5wAdT/y6v0mKV0U2w0WZ2YB/++Tpockg=
github.com ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQCj7ndNxQowgcQnjshcLrqPEiiphnt+VTTvDP6mHBL9j1aNUkY4Ue1gvwnGLVlOhGeYrnZaMgRK6+PKCUXaDbC7qtbW8gIkhL7aGCsOr/C56SJMy/BCZfxd1nWzAOxSDPgVsmerOBYfNqltV9/hWCqBywINIR+5dIg6JTJ72pcEpEjcYgXkE2YEFXV1JHnsKgbLWNlhScqb2UmyRkQyytRLtL+38TGxkxCflmO+5Z8CSSNY7GidjMIZ7Q4zMjA2n1nGrlTDkzwDCsw+wqFPGQA179cnfGWOWRVruj16z6XyvxvjJwbz0wQZ75XK5tKSb7FNyeIEs4TT4jk+S4dhPeAUC5y+bDYirYgM4GC7uEnztnZyaVWQ7B381AK4Qdrwt51ZqExKbQpTUNn+EjqoTwvqNj4kqx5QUCI0ThS/YkOxJCXmPUWZbhjpCg56i+2aB6CmK2JGhn57K5mj0MNdBXA4/WnwH6XoPWJzK5Nyu2zB3nAZp+S5hpQs+p1vN1/wsjk=
`
