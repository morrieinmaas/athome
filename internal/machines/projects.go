package machines

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

func projectsPath(home string) string {
	return filepath.Join(home, ".local", "state", "athome", "remote-projects")
}

// ProjectCommand is the grofe command that opens a repo's session on its box.
func ProjectCommand(m Machine, repo string) []string {
	return []string{"grofe", "-r", m.Name, "new-session", "-A", "-s", repo, "-c", m.Home + "/" + m.Scope + "/" + repo}
}

// WriteRemoteProjects writes one tab-separated line per repo: repo, machine,
// scope, path on the box. A machine whose home is not known yet has no lines.
func WriteRemoteProjects(home string, ms []Machine) error {
	var b strings.Builder
	for _, m := range ms {
		if m.Home == "" {
			continue
		}
		for _, r := range m.Repos {
			fmt.Fprintf(&b, "%s\t%s\t%s\t%s/%s/%s\n", r, m.Name, m.Scope, m.Home, m.Scope, r)
		}
	}
	return writeAtomic(projectsPath(home), []byte(b.String()))
}

// Refresh rewrites the generated ssh config (inventory plus pending adds) and
// the remote projects file (inventory only). laptopKeyFile is the key's base name.
func Refresh(home, laptopKeyFile string) error {
	ms, err := Load(home)
	if err != nil {
		return err
	}
	pend, err := ListPending(home)
	if err != nil {
		return err
	}
	all := append([]Machine(nil), ms...)
	for _, p := range pend {
		if err := p.Validate(); err != nil {
			return err
		}
		if !hasMachine(ms, p.Name) {
			all = append(all, p)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	if err := writeAtomic(filepath.Join(SSHDir(home), "config"), []byte(SSHConfig(all, laptopKeyFile))); err != nil {
		return err
	}
	return WriteRemoteProjects(home, ms)
}

func hasMachine(ms []Machine, name string) bool {
	for _, m := range ms {
		if m.Name == name {
			return true
		}
	}
	return false
}
