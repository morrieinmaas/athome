package machines

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
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
		"gh-delete user/keys athome-acme-dev",
		"gh-delete user/ssh_signing_keys athome-acme-dev-sign",
		"box-gh-logout acme-dev",
		"box-drop-key acme-dev",
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
	if len(w.ops("gh-delete")) != 2 {
		t.Error("the GitHub keys are revoked first, before the box is reached")
	}
	if len(w.ops("grofe machine rm")) != 0 || len(w.ops("keygen-R")) != 0 {
		t.Error("laptop-side steps ran after a failed box step")
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

func TestRemoveOrderGitHubThenBoxThenLaptop(t *testing.T) {
	needKeygen(t)
	w, d, _ := newWorld(t)
	addAcme(t, w, d)
	mark := len(w.logCopy())
	if err := Remove(t.Context(), d, "acme-dev", false); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"gh-delete user/keys athome-acme-dev",
		"gh-delete user/ssh_signing_keys athome-acme-dev-sign",
		"box-gh-logout acme-dev",
		"box-drop-key acme-dev",
		"grofe machine rm acme-dev",
		"keygen-R acme-dev",
		"backup",
	}
	if missing, ok := inOrder(w.logCopy()[mark:], want); !ok {
		t.Fatalf("stuck at %q:\n%s", missing, strings.Join(w.logCopy()[mark:], "\n"))
	}
}

func TestRemoveByNameOnlyReachesNoBox(t *testing.T) {
	w, d, out := newWorld(t)
	w.ghKeys["user/keys"] = []fakeGHKey{{"7", "ssh-ed25519 AAAAX", "athome-ghost"}}
	// the box would answer
	if err := Remove(t.Context(), d, "ghost", true); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := w.sshArgs(); len(got) != 0 {
		t.Errorf("ssh calls for a name that is neither in the inventory nor pending: %v", got)
	}
	if len(w.ops("box-")) != 0 {
		t.Errorf("box ops: %v", w.ops("box-"))
	}
	if len(w.ops("gh-delete")) != 1 {
		t.Errorf("GitHub keys not revoked: %v", w.logCopy())
	}
}

func TestRemoveWithoutIncludeReachesNoBox(t *testing.T) {
	needKeygen(t)
	w, d, out := newWorld(t)
	addAcme(t, w, d)
	if err := os.Remove(filepath.Join(d.Home, ".ssh", "config")); err != nil {
		t.Fatal(err)
	}
	before := len(w.sshArgs())

	err := Remove(t.Context(), d, "acme-dev", false)
	if err == nil || !strings.Contains(err.Error(), "does not include athome/config") {
		t.Fatalf("err = %v", err)
	}
	if err := Remove(t.Context(), d, "acme-dev", true); err != nil {
		t.Fatal(err)
	}
	if got := w.sshArgs(); len(got) != before {
		t.Errorf("ssh calls without the include: %v", got[before:])
	}
	if !strings.Contains(out.String(), "does not include athome/config") {
		t.Errorf("no note:\n%s", out)
	}
}

func TestRemoveNotesTheAccountWhenNoKeyMatched(t *testing.T) {
	t.Run("recorded account", func(t *testing.T) {
		w, d, out := newWorld(t)
		m := acme()
		m.GitHubAccount = "acme-bot"
		if err := Save(d.Home, []Machine{m}); err != nil {
			t.Fatal(err)
		}
		if err := Remove(t.Context(), d, "acme-dev", true); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "note: no GitHub key titled athome-acme-dev or athome-acme-dev-sign in account acme-bot") {
			t.Errorf("no note:\n%s", out)
		}
		_ = w
	})
	t.Run("by name only resolves the active account", func(t *testing.T) {
		_, d, out := newWorld(t)
		if err := Remove(t.Context(), d, "ghost", true); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "in account octo") {
			t.Errorf("no note:\n%s", out)
		}
	})
	t.Run("a match is no note", func(t *testing.T) {
		w, d, out := newWorld(t)
		w.ghKeys["user/keys"] = []fakeGHKey{{"7", "ssh-ed25519 AAAAX", "athome-ghost"}}
		if err := Remove(t.Context(), d, "ghost", true); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "no GitHub key") {
			t.Errorf("note although a key matched:\n%s", out)
		}
	})
}

func TestRemoveAppliesOnlyItsOwnChange(t *testing.T) {
	w, d, _ := newWorld(t)
	if err := Save(d.Home, []Machine{acme()}); err != nil {
		t.Fatal(err)
	}
	w.ghKeys["user/keys"] = []fakeGHKey{{"7", "ssh-ed25519 AAAAX", "athome-acme-dev"}}
	added := false
	d.Run = hookRun{world: w, before: func(c run.Cmd) {
		if added || !slices.Contains(c.Args, "DELETE") {
			return
		}
		added = true
		ms, _ := Load(d.Home)
		if err := Save(d.Home, append(ms, gpu())); err != nil {
			t.Error(err)
		}
	}}
	if err := Remove(t.Context(), d, "acme-dev", false); err != nil {
		t.Fatal(err)
	}
	ms, _ := Load(d.Home)
	if len(ms) != 1 || ms[0].Name != "acme-gpu" {
		t.Fatalf("a machine added meanwhile was lost: %+v", ms)
	}
	cfg, _ := os.ReadFile(filepath.Join(SSHDir(d.Home), "config"))
	if !strings.Contains(string(cfg), "Host acme-gpu") || strings.Contains(string(cfg), "Host acme-dev") {
		t.Errorf("config not refreshed:\n%s", cfg)
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
