package secrets_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/morrieinmaas/athome/internal/run/runtest"
	"github.com/morrieinmaas/athome/internal/secrets"
)

// holdLock takes the secrets lock the way another run would.
func holdLock(t *testing.T, home string) {
	t.Helper()
	p := filepath.Join(home, ".local", "state", "athome", "secrets.lock")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
}

func TestBackupRefusesWhileLocked(t *testing.T) {
	f := newFx(t)
	f.withVault()
	holdLock(t, f.home)
	if code := secrets.Backup(context.Background(), f.opts); code != 1 {
		t.Fatalf("code %d, want 1", code)
	}
	if !strings.Contains(f.errb.String(), "another secrets run is in progress") {
		t.Errorf("stderr %q", f.errb.String())
	}
	if len(f.fake.Calls()) != 0 {
		t.Errorf("ran commands while locked: %v", f.fake.Calls())
	}
}

func TestBackupReleasesLock(t *testing.T) {
	f := newFx(t)
	secrets.Backup(context.Background(), f.opts) // fails: no vault
	secrets.Backup(context.Background(), f.opts)
	if strings.Contains(f.errb.String(), "in progress") {
		t.Errorf("lock was not released: %q", f.errb.String())
	}
}

func TestRekeyRefusesWhileLocked(t *testing.T) {
	g := newRekeyRig(t)
	holdLock(t, g.home)
	if code := secrets.Rekey(context.Background(), g.opts(true, false, "")); code != 1 {
		t.Fatalf("code %d, want 1", code)
	}
	if !strings.Contains(g.errb.String(), "another secrets run is in progress") {
		t.Errorf("stderr %q", g.errb.String())
	}
	if len(g.calls) != 0 {
		t.Errorf("ran commands while locked: %v", g.calls)
	}
}

func TestRekeyHoldsLockThroughout(t *testing.T) {
	g := newRekeyRig(t)
	g.onCall = func(c string) {
		if strings.HasPrefix(c, "gh repo create") {
			var errb bytes.Buffer
			o := secrets.Opts{Home: g.home, Vault: g.vault, Name: "attic", Run: runtest.NewFake(), Err: &errb}
			if code := secrets.Backup(context.Background(), o); code != 1 || !strings.Contains(errb.String(), "in progress") {
				t.Errorf("backup ran during rekey: code %d %q", code, errb.String())
			}
		}
	}
	if code := secrets.Rekey(context.Background(), g.opts(true, false, "")); code != 0 {
		t.Fatalf("code %d, stderr %q", code, g.errb.String())
	}
}
