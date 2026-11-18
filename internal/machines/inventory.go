// Package machines holds the remote-machine inventory and everything that
// reaches a box over ssh.
package machines

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Machine is one inventory entry. Name is the table key, not a field.
type Machine struct {
	Name          string   `toml:"-"`
	Scope         string   `toml:"scope"`
	Target        string   `toml:"target"`
	User          string   `toml:"user"`
	Jump          string   `toml:"jump"`
	Port          int      `toml:"port"`
	Athome        string   `toml:"athome"`
	ForwardAgent  bool     `toml:"forward_agent"`
	GitHubAccount string   `toml:"github_account"`
	Repos         []string `toml:"repos"`
	Home          string   `toml:"home"`
	Hostname      string   `toml:"hostname"`
}

var (
	nameRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
	scopeRe    = regexp.MustCompile(`^(work|personal|sidebiz)(/[A-Za-z0-9._-]+)+$`)
	targetRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*$`)
	userRe     = regexp.MustCompile(`^[a-z_][a-z0-9_.-]*$`)
	jumpRe     = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]*@)?[A-Za-z0-9][A-Za-z0-9._-]*(:[0-9]{1,5})?$`)
	athomeRe   = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	accountRe  = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
	repoRe     = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	homeRe     = regexp.MustCompile(`^/[A-Za-z0-9._/-]*$`)
	hostnameRe = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
)

// Validate checks every field against the character sets that keep a value
// from reaching an ssh config line or a remote shell as anything but itself.
func (m Machine) Validate() error {
	bad := func(field, why string) error {
		return fmt.Errorf("machine %s: %s: %s", m.Name, field, why)
	}
	if !nameRe.MatchString(m.Name) || m.Name == "local" {
		return bad("name", fmt.Sprintf("%q must match %s and not be local", m.Name, nameRe))
	}
	if !scopeRe.MatchString(m.Scope) || slices.ContainsFunc(strings.Split(m.Scope, "/"), func(s string) bool { return s == "." || s == ".." }) {
		return bad("scope", fmt.Sprintf("%q must be work/, personal/ or sidebiz/ followed by path segments of [A-Za-z0-9._-]", m.Scope))
	}
	if !targetRe.MatchString(m.Target) {
		return bad("target", fmt.Sprintf("%q must match %s", m.Target, targetRe))
	}
	if m.User == "root" {
		return fmt.Errorf("machine %s: user root: log in as a sudo user; the bootstrap turns off root login", m.Name)
	}
	if !userRe.MatchString(m.User) {
		return bad("user", fmt.Sprintf("%q must match %s", m.User, userRe))
	}
	if m.Jump != "" && !jumpRe.MatchString(m.Jump) {
		return bad("jump", fmt.Sprintf("%q must be [user@]host[:port], starting with a letter or digit", m.Jump))
	}
	if m.Port < 0 || m.Port > 65535 {
		return bad("port", fmt.Sprintf("%d must be 1 to 65535", m.Port))
	}
	if !athomeRe.MatchString(m.Athome) {
		return bad("athome", fmt.Sprintf("%q must look like v1.2.3", m.Athome))
	}
	if m.GitHubAccount != "" && !accountRe.MatchString(m.GitHubAccount) {
		return bad("github_account", fmt.Sprintf("%q must match %s", m.GitHubAccount, accountRe))
	}
	seen := map[string]bool{}
	for _, r := range m.Repos {
		if !repoRe.MatchString(r) || r == "." || r == ".." {
			return bad("repos", fmt.Sprintf("%q must match %s and not be . or ..", r, repoRe))
		}
		if seen[r] {
			return bad("repos", fmt.Sprintf("%q is listed twice", r))
		}
		seen[r] = true
	}
	if m.Home != "" && !homeRe.MatchString(m.Home) {
		return bad("home", fmt.Sprintf("%q must be an absolute path of [A-Za-z0-9._/-]", m.Home))
	}
	if m.Hostname != "" && !hostnameRe.MatchString(m.Hostname) {
		return bad("hostname", fmt.Sprintf("%q must match %s", m.Hostname, hostnameRe))
	}
	return nil
}

// Context is the scope's first segment: "work" for "work/acme".
func (m Machine) Context() string {
	c, _, _ := strings.Cut(m.Scope, "/")
	return c
}

func (m Machine) port() int {
	if m.Port == 0 {
		return 22
	}
	return m.Port
}

func Path(home string) string { return filepath.Join(home, ".config", "athome", "machines.toml") }

type inventoryFile struct {
	Machines map[string]Machine `toml:"machines"`
}

// Load reads and validates the inventory, in name order. A missing file is
// an empty inventory.
func Load(home string) ([]Machine, error) {
	p := Path(home)
	var f inventoryFile
	if _, err := toml.DecodeFile(p, &f); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	names := make([]string, 0, len(f.Machines))
	for n := range f.Machines {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []Machine
	for _, n := range names {
		m := f.Machines[n]
		m.Name = n
		if err := m.Validate(); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// Save writes the inventory (0600, directory 0700). It leaves the generated
// ssh config and remote projects file alone; Deps.updateInventory refreshes them.
func Save(home string, ms []Machine) error {
	f := inventoryFile{Machines: map[string]Machine{}}
	for _, m := range ms {
		if err := m.Validate(); err != nil {
			return err
		}
		if _, dup := f.Machines[m.Name]; dup {
			return fmt.Errorf("machine %s: name: listed twice", m.Name)
		}
		f.Machines[m.Name] = m
	}
	var b strings.Builder
	if err := toml.NewEncoder(&b).Encode(f); err != nil {
		return err
	}
	return writeAtomic(Path(home), []byte(b.String()))
}

// writeAtomic writes data 0600 through a temp file and rename, creating the
// directory 0700.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

const localScopeDepth = 4

// CheckLocalScope refuses a scope whose ~/<scope> holds a .git up to four
// levels down; a scope is local or remote, never both.
func CheckLocalScope(home, scope string) error {
	root := filepath.Join(home, filepath.FromSlash(scope))
	var first string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			return nil
		}
		depth := strings.Count(rel, string(filepath.Separator)) + 1
		if d.Name() == ".git" {
			first = p
			return fs.SkipAll
		}
		if d.IsDir() && depth >= localScopeDepth {
			return fs.SkipDir
		}
		return nil
	})
	if first == "" {
		return nil
	}
	rel, _ := filepath.Rel(home, first)
	return fmt.Errorf("~/%s holds git repositories (~/%s); a scope is local or remote, never both", scope, filepath.ToSlash(rel))
}

// ScopeWarnings is the local scope check for each distinct inventory scope.
func ScopeWarnings(home string, ms []Machine) []string {
	var out []string
	done := map[string]bool{}
	for _, m := range ms {
		if done[m.Scope] {
			continue
		}
		done[m.Scope] = true
		if err := CheckLocalScope(home, m.Scope); err != nil {
			out = append(out, err.Error())
		}
	}
	return out
}
