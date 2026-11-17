package status

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestKeysReminder(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	home := t.TempDir()
	state := filepath.Join(home, ".local", "state", "athome", "rotate.json")
	os.MkdirAll(filepath.Dir(state), 0o755)
	os.WriteFile(state, []byte(`{"jobs":[],"last_rotated":"2020-01-01T00:00:00Z"}`), 0o600)
	if k := Keys(home, "h", now); k != (KeysStatus{}) {
		t.Errorf("no key: %+v", k)
	}
	os.Remove(state)
	key := filepath.Join(home, ".ssh", "h_ed25519")
	os.MkdirAll(filepath.Dir(key), 0o700)
	os.WriteFile(key, []byte("k"), 0o600)
	old := now.Add(-100 * 24 * time.Hour)
	os.Chtimes(key, old, old)
	k := Keys(home, "h", now)
	if !k.Due || k.DaysSince(now) != 100 {
		t.Errorf("old key: %+v, %d days", k, k.DaysSince(now))
	}

	os.WriteFile(state, []byte(`{"jobs":[],"last_rotated":"`+now.Add(-24*time.Hour).Format(time.RFC3339)+`"}`), 0o600)
	if k := Keys(home, "h", now); k.Due || k.LastRotated != "2026-11-16T12:00:00Z" {
		t.Errorf("rotated yesterday: %+v", k)
	}
}

func TestKeysInJSON(t *testing.T) {
	b, _ := json.Marshal(Report{})
	if !strings.Contains(string(b), `"keys":{"last_rotated":"","due":false}`) {
		t.Errorf("json = %s", b)
	}
}
