package machines

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seedGitHubDecoys(w *world) {
	w.ghKeys["user/keys"] = []fakeGHKey{
		{"900", "ssh-ed25519 AAAAUNRELATED", "my laptop"},
		{"901", "ssh-ed25519 AAAADECOY", "athome-acme-dev-old"},
	}
	w.ghKeys["user/ssh_signing_keys"] = []fakeGHKey{{"902", "ssh-ed25519 AAAAUNRELATED", "my laptop"}}
}

func addAcme(t *testing.T, w *world, d Deps) {
	t.Helper()
	if err := Add(context.Background(), d, newAdd()); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveRevokesEverything(t *testing.T) {
	needKeygen(t)
	w, d, _ := newWorld(t)
	seedGitHubDecoys(w)
	addAcme(t, w, d)
	before := len(w.ops("backup"))
	mark := len(w.logCopy())

	if err := Remove(context.Background(), d, "acme-dev", false); err != nil {
		t.Fatal(err)
	}
	log := w.logCopy()[mark:]
	want := []string{
		"box-gh-logout acme-dev",
		"box-drop-key acme-dev",
		"gh-delete user/keys athome-acme-dev",
		"gh-delete user/ssh_signing_keys athome-acme-dev-sign",
		"grofe machine rm acme-dev",
		"keygen-R acme-dev",
		"backup",
	}
	if missing, ok := inOrder(log, want); !ok {
		t.Fatalf("stuck at %q:\n%s", missing, strings.Join(log, "\n"))
	}
	if len(w.ops("backup")) != before+1 {
		t.Error("no backup after the inventory changed")
	}
	if n := len(w.ops("gh-delete")); n != 2 {
		t.Errorf("deleted %d keys: %v", n, w.ops("gh-delete"))
	}
	if len(w.ghKeys["user/keys"]) != 2 || len(w.ghKeys["user/ssh_signing_keys"]) != 1 {
		t.Errorf("unrelated keys touched: %+v", w.ghKeys)
	}
	if strings.Contains(w.boxes["acme-dev"].authkeys, "ssh-ed25519") {
		t.Errorf("break-glass key still authorized: %q", w.boxes["acme-dev"].authkeys)
	}
	if len(w.rbwItems["athome/work/acme/break-glass"]) != 1 {
		t.Error("the break-glass item must stay in Bitwarden")
	}
	if ms, _ := Load(d.Home); len(ms) != 0 {
		t.Errorf("entry left: %v", ms)
	}
	cfg, _ := os.ReadFile(filepath.Join(d.Home, ".ssh", "athome", "config"))
	if strings.Contains(string(cfg), "Host acme-dev") {
		t.Errorf("block left:\n%s", cfg)
	}
	kh, _ := os.ReadFile(KnownHosts(d.Home))
	if strings.Contains(string(kh), "acme-dev") {
		t.Errorf("host key left: %s", kh)
	}
}

func TestRemoveUnknownAndFailing(t *testing.T) {
	needKeygen(t)
	w, d, _ := newWorld(t)
	if err := Remove(context.Background(), d, "ghost", false); err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("unknown: %v", err)
	}
	addAcme(t, w, d)
	w.box("acme-dev").down = true
	err := Remove(context.Background(), d, "acme-dev", false)
	if err == nil || !strings.HasPrefix(err.Error(), "athome machine rm: gh auth logout on the box: ") {
		t.Fatalf("err = %v", err)
	}
	if ms, _ := Load(d.Home); len(ms) != 1 {
		t.Error("inventory changed although a step failed")
	}
	if len(w.ops("gh-delete")) != 0 {
		t.Error("GitHub keys deleted after a failed box step")
	}
}

func TestRemoveForceAfterPartialAdd(t *testing.T) {
	needKeygen(t)
	w, d, out := newWorld(t)
	w.setFail("bootstrap", true)
	if err := Add(context.Background(), d, newAdd()); err == nil {
		t.Fatal("add should fail")
	}
	w.box("acme-dev").down = true
	if p, _ := LoadPending(d.Home, "acme-dev"); p == nil || p.Home == "" {
		t.Fatal("no pending entry")
	}
	if err := Remove(context.Background(), d, "acme-dev", true); err != nil {
		t.Fatalf("force must exit 0: %v", err)
	}
	if !strings.Contains(out.String(), "note: gh auth logout on the box: ") {
		t.Errorf("no note for the unreachable box:\n%s", out)
	}
	if got := w.ops("gh-delete"); len(got) != 2 {
		t.Errorf("GitHub deletes: %v", got)
	}
	if len(w.ops("keygen-R acme-dev")) != 1 {
		t.Error("host key not removed")
	}
	if p, _ := LoadPending(d.Home, "acme-dev"); p != nil {
		t.Error("pending left")
	}
	cfg, _ := os.ReadFile(filepath.Join(d.Home, ".ssh", "athome", "config"))
	if strings.Contains(string(cfg), "acme-dev") {
		t.Errorf("block left:\n%s", cfg)
	}
}

func TestRemoveForceByNameOnly(t *testing.T) {
	w, d, out := newWorld(t)
	w.ghKeys["user/keys"] = []fakeGHKey{{"7", "ssh-ed25519 AAAAX", "athome-ghost"}}
	w.grofe["ghost"] = "ghost"
	w.box("ghost").down = true
	os.MkdirAll(SSHDir(d.Home), 0o700)
	os.WriteFile(KnownHosts(d.Home), []byte("ghost ssh-ed25519 AAAAFAKEHOSTKEY\n"), 0o600)
	if err := Remove(context.Background(), d, "ghost", true); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"gh-delete user/keys athome-ghost", "grofe machine rm ghost", "keygen-R ghost"} {
		if len(w.ops(want)) != 1 {
			t.Errorf("missing %q:\n%s", want, strings.Join(w.logCopy(), "\n"))
		}
	}
	if len(w.ops("backup")) != 0 {
		t.Error("backup ran although the inventory did not change")
	}
	if err := Remove(context.Background(), d, "../x", true); err == nil {
		t.Error("a bad name was accepted")
	}
}

func TestRemoveUsesGitHubAccount(t *testing.T) {
	w, d, _ := newWorld(t)
	m := acme()
	m.GitHubAccount = "acme-bot"
	Save(d.Home, []Machine{m})
	if err := Remove(context.Background(), d, "acme-dev", true); err != nil {
		t.Fatal(err)
	}
	if len(w.ghEnv) == 0 || w.ghEnv[0] != "tok-acme-bot" {
		t.Errorf("ghEnv = %v", w.ghEnv)
	}
}
