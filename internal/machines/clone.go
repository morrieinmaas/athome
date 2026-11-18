package machines

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var (
	ownerNameRe = regexp.MustCompile(`^([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+)$`)
	sshURLRe    = regexp.MustCompile(`^git@github\.com:([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+?)(\.git)?$`)
	httpsURLRe  = regexp.MustCompile(`^https://github\.com/([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+?)(\.git)?$`)
)

// parseRepo accepts owner/name, a git@github.com: URL or an https://github.com/ URL.
// The name "worktrees" is refused: scope/worktrees holds the worktree layout.
func parseRepo(s string) (url, name string, err error) {
	var owner string
	if m := ownerNameRe.FindStringSubmatch(s); m != nil {
		owner, name = m[1], strings.TrimSuffix(m[2], ".git")
	} else if m := sshURLRe.FindStringSubmatch(s); m != nil {
		owner, name = m[1], m[2]
	} else if m := httpsURLRe.FindStringSubmatch(s); m != nil {
		owner, name = m[1], m[2]
	}
	if owner == "" || name == "" || name == "." || name == ".." || name == "worktrees" || owner == "." || owner == ".." {
		return "", "", UsageError(fmt.Sprintf("%q is not owner/name, git@github.com:owner/name.git or https://github.com/owner/name", s))
	}
	return "git@github.com:" + owner + "/" + name + ".git", name, nil
}

const (
	mkWorktreesScript = `mkdir -p "$1/worktrees"`
	// cloneScript pins GitHub's host keys first: the box's ~/.ssh/config may not be rendered yet.
	cloneScript      = pinGitHubHosts + `[ -d "$1/$2/.git" ] || GIT_SSH_COMMAND='ssh -o UserKnownHostsFile="$HOME/.ssh/athome_github_known_hosts" -o StrictHostKeyChecking=yes' git clone "$3" "$1/$2"`
	miseDetectScript = `cd "$1/$2" || exit 1
for f in mise.toml .mise.toml .tool-versions; do [ -f "$f" ] && echo yes && exit 0; done
exit 0`
	miseInstallScript = `cd "$1/$2" && "$HOME/.local/bin/mise" trust && "$HOME/.local/bin/mise" install`
)

// Clone clones repo into the machine's scope directory, installs its tools,
// copies its .env files and records it in the machine's repos.
func Clone(ctx context.Context, d Deps, repo, machine string) error {
	url, name, err := parseRepo(repo)
	if err != nil {
		return err
	}
	ms, err := Load(d.Home)
	if err != nil {
		return err
	}
	idx := -1
	if machine == "" {
		if len(ms) != 1 {
			var names []string
			for _, m := range ms {
				names = append(names, m.Name)
			}
			if len(ms) == 0 {
				return UsageError("no machines; add one with athome machine add")
			}
			return UsageError("more than one machine (" + strings.Join(names, ", ") + "); pick one with --machine")
		}
		idx = 0
	} else {
		idx = slices.IndexFunc(ms, func(m Machine) bool { return m.Name == machine })
		if idx < 0 {
			return fmt.Errorf("machine %s not found", machine)
		}
	}
	m := ms[idx]
	if m.Home == "" {
		return fmt.Errorf("machine %s has no home recorded; rerun athome machine add %s", m.Name, m.Name)
	}
	if err := CheckInclude(d.Home); err != nil {
		return err
	}
	r := d.remote()
	scopeDir := m.Home + "/" + m.Scope
	out := d.outOrDiscard()

	res, err := r.Exec(ctx, m.Name, mkWorktreesScript, "", scopeDir)
	if err := okOrErr("create "+scopeDir+"/worktrees", m.Name, res, err); err != nil {
		return err
	}
	res, err = r.Exec(ctx, m.Name, cloneScript, githubKnownHosts, scopeDir, name, url)
	if err := okOrErr("clone "+url, m.Name, res, err); err != nil {
		return err
	}
	res, err = r.Exec(ctx, m.Name, miseDetectScript, "", scopeDir, name)
	if err := okOrErr("look for mise config", m.Name, res, err); err != nil {
		return err
	}
	if strings.TrimSpace(res.Stdout) == "yes" {
		res, err = r.Exec(ctx, m.Name, miseInstallScript, "", scopeDir, name)
		if err := okOrErr("mise install", m.Name, res, err); err != nil {
			return err
		}
	}
	if err := copyEnv(ctx, d, r.Exec, m, name, out); err != nil {
		return err
	}
	changed, err := d.updateInventory(func(ms []Machine) ([]Machine, bool, error) {
		i := slices.IndexFunc(ms, func(x Machine) bool { return x.Name == m.Name })
		if i < 0 {
			return nil, false, fmt.Errorf("machine %s was removed while %s was being cloned", m.Name, name)
		}
		if slices.Contains(ms[i].Repos, name) {
			return ms, false, nil
		}
		ms[i].Repos = append(ms[i].Repos, name)
		return ms, true, nil
	})
	if err != nil {
		return err
	}
	if changed {
		d.backup(ctx)
	}
	d.say("✓ %s on %s: %s/%s", name, m.Name, scopeDir, name)
	return nil
}
