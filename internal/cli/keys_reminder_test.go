package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/morrieinmaas/athome/internal/keys"
	"github.com/morrieinmaas/athome/internal/steps"
)

func TestKeysReminderForHome(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	if r := keysReminder(home, now); r != "" {
		t.Errorf("no key: %q", r)
	}
	key := keys.LaptopKey(home, steps.KeyHostname())
	os.MkdirAll(filepath.Dir(key), 0o700)
	os.WriteFile(key, []byte("k"), 0o600)
	old := now.Add(-95 * 24 * time.Hour)
	os.Chtimes(key, old, old)
	if r := keysReminder(home, now); !strings.Contains(r, "last rotated 95 days ago") {
		t.Errorf("reminder = %q", r)
	}
}
