package machines

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func generatedConfig(home string) string {
	b, _ := os.ReadFile(filepath.Join(SSHDir(home), "config"))
	return string(b)
}

// Only the Include line is not enough: without ~/.ssh/athome/config ssh would
// resolve the bare name outside the pinned HostKeyAlias block.
func TestBoxCommandsWriteTheGeneratedConfigFirst(t *testing.T) {
	missing := func(t *testing.T) (*world, Deps) {
		w, d, _ := newWorld(t)
		if err := Save(d.Home, []Machine{acme()}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(SSHDir(d.Home), "config")); err == nil {
			t.Fatal("the generated config exists before the command")
		}
		return w, d
	}
	t.Run("sync", func(t *testing.T) {
		_, d := missing(t)
		if err := Sync(t.Context(), d, "acme-dev", false); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(generatedConfig(d.Home), "Host acme-dev") {
			t.Error("no Host block after sync")
		}
	})
	t.Run("clone", func(t *testing.T) {
		_, d := missing(t)
		if err := Clone(t.Context(), d, "acme/api", "acme-dev"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(generatedConfig(d.Home), "Host acme-dev") {
			t.Error("no Host block after clone")
		}
	})
	t.Run("keys inventory", func(t *testing.T) {
		w, d := missing(t)
		if _, err := KeysInventory(d.Home, w, "lap"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(generatedConfig(d.Home), "Host acme-dev") {
			t.Error("no Host block after KeysInventory")
		}
	})
	t.Run("remove", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("root ignores directory modes")
		}
		_, d := missing(t)
		// the inventory write fails, so the config the box steps wrote stays
		blocked := filepath.Join(d.Home, ".config", "athome")
		os.Chmod(blocked, 0o500)
		t.Cleanup(func() { os.Chmod(blocked, 0o700) })
		if err := Remove(t.Context(), d, "acme-dev", false); err == nil {
			t.Fatal("want the inventory step to fail")
		}
		if !strings.Contains(generatedConfig(d.Home), "Host acme-dev") {
			t.Error("no Host block when the box was reached")
		}
	})
}

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

func TestRemoveOfAnInvalidPendingEntryNeedsForce(t *testing.T) {
	w, d, _ := newWorld(t)
	writePending(t, d.Home, "bad", badPending)
	if err := Remove(context.Background(), d, "bad", false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("err = %v", err)
	}
	if err := Remove(context.Background(), d, "bad", true); err != nil {
		t.Fatalf("force: %v", err)
	}
	if _, err := os.Stat(filepath.Join(pendingDir(d.Home), "bad.toml")); err == nil {
		t.Error("invalid pending file left")
	}
	for _, c := range w.calls {
		if c.Name == "ssh" {
			t.Errorf("a box was reached for an entry that says nothing about one: %v", c.Args)
		}
	}
}
