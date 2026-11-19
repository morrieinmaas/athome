package machines

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePending(t *testing.T, home, name, body string) {
	t.Helper()
	if err := os.MkdirAll(pendingDir(home), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pendingDir(home), name+".toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

const badPending = "scope = \"home/x\"\ntarget = \"x.example\"\nuser = \"dev\"\n"

func TestLoadPendingValidates(t *testing.T) {
	home := t.TempDir()
	writePending(t, home, "bad", badPending)
	_, err := LoadPending(home, "bad")
	var inv *InvalidPendingError
	if !errors.As(err, &inv) || !strings.Contains(err.Error(), "athome machine rm bad --force") {
		t.Fatalf("err = %v", err)
	}
}

func TestRefreshSkipsAnInvalidPendingEntryWithANote(t *testing.T) {
	home := t.TempDir()
	if err := SavePending(home, newAdd()); err != nil {
		t.Fatal(err)
	}
	writePending(t, home, "bad", badPending)
	var note bytes.Buffer
	if err := RefreshTo(home, "laptop_ed25519", &note); err != nil {
		t.Fatalf("refresh failed on an invalid pending entry: %v", err)
	}
	cfg := generatedConfig(home)
	if !strings.Contains(cfg, "Host acme-dev") || strings.Contains(cfg, "Host bad") {
		t.Errorf("config:\n%s", cfg)
	}
	if !strings.Contains(note.String(), "bad") {
		t.Errorf("no note: %q", note.String())
	}
}
