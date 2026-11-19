package machines

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/morrieinmaas/athome/internal/keys"
	"github.com/morrieinmaas/athome/internal/run"
)

// ghClient runs gh for one machine. With a github_account every call carries
// GH_TOKEN from that account, in the environment only, never in args or output.
type ghClient struct {
	r       run.Runner
	account string
	tokened run.Runner // r with the account's GH_TOKEN, once fetched
}

func (g *ghClient) runner(ctx context.Context) (run.Runner, error) {
	if g.account == "" {
		return g.r, nil
	}
	if g.tokened == nil {
		r, err := keys.GHTokenRunner(ctx, g.r, g.account)
		var nt *keys.NoTokenError
		if errors.As(err, &nt) {
			return nil, fmt.Errorf("no gh token for account %s: %s", nt.Account, nt.Stderr)
		}
		if err != nil {
			return nil, err
		}
		g.tokened = r
	}
	return g.tokened, nil
}

func (g *ghClient) run(ctx context.Context, args ...string) (run.Result, error) {
	r, err := g.runner(ctx)
	if err != nil {
		return run.Result{}, err
	}
	return r.Run(ctx, run.Cmd{Name: "gh", Args: args})
}

// keys lists the account's keys, with titles, at endpoint (one of keys.GHKeyEndpoints).
func (g *ghClient) keys(ctx context.Context, endpoint string) ([]keys.GHKey, error) {
	r, err := g.runner(ctx)
	if err != nil {
		return nil, err
	}
	return keys.GHKeys(ctx, r, endpoint, true)
}

func hasBlob(ks []keys.GHKey, blob string) bool {
	return slices.ContainsFunc(ks, func(k keys.GHKey) bool { return k.Blob == blob })
}

// registered reports whether blob is on GitHub as an authentication and as a signing key.
func (g *ghClient) registered(ctx context.Context, blob string) (auth, sign bool, err error) {
	ak, err := g.keys(ctx, keys.GHKeyEndpoints[0])
	if err != nil {
		return false, false, err
	}
	sk, err := g.keys(ctx, keys.GHKeyEndpoints[1])
	if err != nil {
		return false, false, err
	}
	return hasBlob(ak, blob), hasBlob(sk, blob), nil
}

// register adds whichever of the two keys pub is missing as, titled athome-<name>[-sign].
func (g *ghClient) register(ctx context.Context, name, pub string) error {
	auth, sign, err := g.registered(ctx, keys.PubBlob(pub))
	if err != nil {
		return err
	}
	err = keys.WithPubFile(pub, func(path string) error {
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
			args := append([]string{"ssh-key", "add", path, "--title", a.title}, a.extra...)
			res, err := g.run(ctx, args...)
			if err != nil {
				return err
			}
			// "already" may mean the key is on another account, so the listing decides below.
			if res.Code != 0 && !strings.Contains(res.Stderr, "already") {
				return fmt.Errorf("gh ssh-key add --title %s failed: %s (missing scopes? run: %s)", a.title, strings.TrimSpace(res.Stderr), keys.ScopeHint)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	auth, sign, err = g.registered(ctx, keys.PubBlob(pub))
	if err != nil {
		return err
	}
	if !auth || !sign {
		return fmt.Errorf("the key of %s is not on GitHub account %s as both an authentication and a signing key (is it registered on another account?)", name, g.accountName())
	}
	return nil
}

func (g *ghClient) accountName() string {
	if g.account == "" {
		return "gh is logged in to"
	}
	return g.account
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
		{keys.GHKeyEndpoints[0], "athome-" + name},
		{keys.GHKeyEndpoints[1], "athome-" + name + "-sign"},
	} {
		ks, err := g.keys(ctx, e.endpoint)
		if err != nil {
			return n, err
		}
		for _, k := range ks {
			if k.Title != e.title {
				continue
			}
			r, err := g.runner(ctx)
			if err != nil {
				return n, err
			}
			if err := keys.GHDeleteKey(ctx, r, e.endpoint, k.ID); err != nil {
				return n, err
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
