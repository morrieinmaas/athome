package keys

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/run/runtest"
)

const pem = "fake private key\nsecretbytes\nend"

func TestLaptopKeyPrefersID(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	if got := LaptopKey(home, "h"); got != filepath.Join(home, ".ssh", "h_ed25519") {
		t.Errorf("no id_ed25519: got %s", got)
	}
	os.WriteFile(filepath.Join(home, ".ssh", "id_ed25519"), []byte("k"), 0o600)
	if got := LaptopKey(home, "h"); got != filepath.Join(home, ".ssh", "id_ed25519") {
		t.Errorf("with id_ed25519: got %s", got)
	}
	if ItemFor(filepath.Join(home, ".ssh", "id_ed25519")) != "ssh/id_ed25519" {
		t.Errorf("ItemFor")
	}
}

func noTempLeft(t *testing.T) {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(os.TempDir(), "athome-rbw-*"))
	if len(m) > 0 {
		t.Errorf("temp files left: %v", m)
	}
}

func TestStoreAddsThenEdits(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	f := newFakeBW(t)
	ctx := context.Background()
	if err := Store(ctx, f, "ssh/h_ed25519", SSHKeyPassword, pem); err != nil {
		t.Fatal(err)
	}
	if err := Store(ctx, f, "ssh/h_ed25519", SSHKeyPassword, pem+"2"); err != nil {
		t.Fatal(err)
	}
	items := f.get("ssh/h_ed25519")
	if len(items) != 1 || items[0] != SSHKeyPassword+"\n"+pem+"2\n" {
		t.Fatalf("items = %q", items)
	}
	calls := strings.Join(f.bwCalls, "|")
	if !strings.Contains(calls, "add ssh/h_ed25519") || !strings.Contains(calls, "edit ssh/h_ed25519") {
		t.Errorf("calls = %s", calls)
	}
	for _, c := range f.Calls() {
		for _, a := range append(c.Args, c.Stdin) {
			if strings.Contains(a, "secretbytes") {
				t.Errorf("secret on a command line: %+v", c)
			}
		}
	}
	noTempLeft(t)
}

func TestStoreRefusesDuplicates(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	f := newFakeBW(t)
	f.put("ssh/h_ed25519", "a\n")
	f.put("ssh/h_ed25519", "b\n")
	err := Store(context.Background(), f, "ssh/h_ed25519", SSHKeyPassword, pem)
	if err == nil || !strings.Contains(err.Error(), "delete the extras") {
		t.Fatalf("err = %v", err)
	}
	for _, c := range f.bwCalls {
		if strings.HasPrefix(c, "add") || strings.HasPrefix(c, "edit") {
			t.Errorf("wrote despite duplicates: %s", c)
		}
	}
}

func TestStoreLocked(t *testing.T) {
	f := newFakeBW(t)
	f.locked = true
	err := Store(context.Background(), f, "x", "p", "n")
	if err == nil || !strings.Contains(err.Error(), "rbw is locked") {
		t.Fatalf("err = %v", err)
	}
	if len(f.bwCalls) != 1 {
		t.Errorf("calls after locked: %v", f.bwCalls)
	}
}

func TestStoreListFails(t *testing.T) {
	f := newFakeBW(t)
	f.listFails = true
	if err := Store(context.Background(), f, "x", "p", "n"); err == nil {
		t.Fatal("want an error when rbw list fails")
	}
	if len(f.get("x")) != 0 {
		t.Error("stored despite the failed list")
	}
}

func TestStoreWithoutRbw(t *testing.T) {
	f := newFakeBW(t)
	f.Fake = runtest.NewFake() // no rbw on PATH
	if err := Store(context.Background(), f, "x", "p", "n"); !errors.Is(err, ErrNoRbw) {
		t.Errorf("err = %v, want ErrNoRbw (not a locked message)", err)
	}
}
