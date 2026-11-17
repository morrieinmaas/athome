package keys

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	kindLaptop     = "laptop"
	kindBreakGlass = "break-glass"

	phaseAdded   = "added"
	phaseChecked = "checked"
	phaseRemoved = "removed"
	phaseSkipped = "skipped"
)

// job is one key being rotated. Targets maps "github" or "box:<name>" to the
// last phase that target passed.
type job struct {
	Kind      string            `json:"kind"`
	Scope     string            `json:"scope"`
	Item      string            `json:"item"`
	Key       string            `json:"key"`
	OldPub    string            `json:"old_pub"`
	NewPub    string            `json:"new_pub"`
	Stored    bool              `json:"stored"`
	Installed bool              `json:"installed"`
	Targets   map[string]string `json:"targets"`
}

type state struct {
	Jobs        []*job `json:"jobs"`
	LastRotated string `json:"last_rotated"`
}

// StatePath is the rotation progress file.
func StatePath(home string) string {
	return filepath.Join(home, ".local", "state", "athome", "rotate.json")
}

func loadState(home string) (*state, error) {
	b, err := os.ReadFile(StatePath(home))
	if errors.Is(err, os.ErrNotExist) {
		return &state{}, nil
	}
	if err != nil {
		return nil, err
	}
	var s state
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON (%v); fix or remove it", StatePath(home), err)
	}
	return &s, nil
}

func (s *state) save(home string) error {
	p := StatePath(home)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// LastRotated is when the last rotation finished, from the progress file.
func LastRotated(home string) (time.Time, bool) {
	s, err := loadState(home)
	if err != nil || s.LastRotated == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s.LastRotated)
	return t, err == nil
}
