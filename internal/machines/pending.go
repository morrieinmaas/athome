package machines

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

func pendingDir(home string) string {
	return filepath.Join(home, ".local", "state", "athome", "machines")
}

func pendingPath(home, name string) (string, error) {
	if !nameRe.MatchString(name) {
		return "", fmt.Errorf("machine %s: name: must match %s", name, nameRe)
	}
	return filepath.Join(pendingDir(home), name+".toml"), nil
}

// LoadPending reads the half-added machine's entry; nil, nil when there is none.
func LoadPending(home, name string) (*Machine, error) {
	p, err := pendingPath(home, name)
	if err != nil {
		return nil, err
	}
	var m Machine
	if _, err := toml.DecodeFile(p, &m); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	m.Name = name
	if m.Port == 0 {
		m.Port = defaultPort
	}
	return &m, nil
}

func SavePending(home string, m Machine) error {
	p, err := pendingPath(home, m.Name)
	if err != nil {
		return err
	}
	var b strings.Builder
	if err := toml.NewEncoder(&b).Encode(m); err != nil {
		return err
	}
	return writeAtomic(p, []byte(b.String()))
}

func RemovePending(home, name string) error {
	p, err := pendingPath(home, name)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// ListPending reads every pending entry, in name order.
func ListPending(home string) ([]Machine, error) {
	files, err := os.ReadDir(pendingDir(home))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Machine
	for _, f := range files {
		name, ok := strings.CutSuffix(f.Name(), ".toml")
		if !ok {
			continue
		}
		m, err := LoadPending(home, name)
		if err != nil {
			return nil, err
		}
		if m != nil {
			out = append(out, *m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
