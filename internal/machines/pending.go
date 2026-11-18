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

// InvalidPendingError is a pending file that does not parse or does not
// validate. Nothing but rm --force can use it; Refresh skips it.
type InvalidPendingError struct {
	Name string
	Err  error
}

func (e *InvalidPendingError) Error() string {
	return fmt.Sprintf("the pending add of %s is invalid (%v); remove it with athome machine rm %s --force", e.Name, e.Err, e.Name)
}

func (e *InvalidPendingError) Unwrap() error { return e.Err }

// LoadPending reads the half-added machine's entry; nil, nil when there is none.
// An entry that does not parse or validate is an *InvalidPendingError.
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
		return nil, &InvalidPendingError{name, fmt.Errorf("%s: %w", p, err)}
	}
	m.Name = name
	if m.Port == 0 {
		m.Port = defaultPort
	}
	if err := m.Validate(); err != nil {
		return nil, &InvalidPendingError{name, err}
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

// ListPending reads every valid pending entry, in name order. An entry that
// is invalid is left out and reported in bad.
func ListPending(home string) (out []Machine, bad []error, err error) {
	files, err := os.ReadDir(pendingDir(home))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	for _, f := range files {
		name, ok := strings.CutSuffix(f.Name(), ".toml")
		if !ok {
			continue
		}
		if !nameRe.MatchString(name) {
			bad = append(bad, &InvalidPendingError{name, fmt.Errorf("name: must match %s", nameRe)})
			continue
		}
		m, err := LoadPending(home, name)
		var inv *InvalidPendingError
		if errors.As(err, &inv) {
			bad = append(bad, err)
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		if m != nil {
			out = append(out, *m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, bad, nil
}
