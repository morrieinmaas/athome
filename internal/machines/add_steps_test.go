package machines

import (
	"strings"
	"testing"
)

func TestAddStoresTheResolvedAccountAndRmUsesIt(t *testing.T) {
	needKeygen(t)
	w, d, _ := newWorld(t)
	w.setFail("probe", true)
	if err := Add(t.Context(), d, newAdd()); err == nil {
		t.Fatal("expected the probe failure")
	}
	if p, _ := LoadPending(d.Home, "acme-dev"); p == nil || p.GitHubAccount != "octo" {
		t.Fatalf("pending entry lacks the account: %+v", p)
	}
	w.setFail("probe", false)
	if err := Add(t.Context(), d, newAdd()); err != nil { // same flags: still the same entry
		t.Fatal(err)
	}
	ms, _ := Load(d.Home)
	if len(ms) != 1 || ms[0].GitHubAccount != "octo" {
		t.Fatalf("inventory: %+v", ms)
	}
	for _, v := range w.ghEnv {
		if v != "tok-octo" {
			t.Errorf("GH_TOKEN = %q", v)
		}
	}
	if len(w.ops("gh-user")) != 1 {
		t.Errorf("account resolved %d times", len(w.ops("gh-user")))
	}

	w.ghLogin = "switched" // gh auth switch
	mark := len(w.ghEnv)
	if err := Remove(t.Context(), d, "acme-dev", false); err != nil {
		t.Fatal(err)
	}
	if len(w.ghEnv) == mark {
		t.Fatal("rm made no gh call with a token")
	}
	for _, v := range w.ghEnv {
		if v != "tok-octo" {
			t.Errorf("GH_TOKEN = %q after the switch", v)
		}
	}
	if len(w.ops("gh-user")) != 1 {
		t.Errorf("account resolved again by rm: %v", w.ops("gh-user"))
	}
}

func TestAddFailsWhenTheAccountCannotBeResolved(t *testing.T) {
	w, d, _ := newWorld(t)
	w.ghLogin = ""
	err := Add(t.Context(), d, newAdd())
	if err == nil || !strings.Contains(err.Error(), "machine-validate") || !strings.Contains(err.Error(), "GitHub account") {
		t.Fatalf("err = %v", err)
	}
	if len(w.sshArgs()) != 0 {
		t.Error("a box was reached")
	}
}

func TestAuthorizedHasSkipsComments(t *testing.T) {
	blob := blobOf(pub1)
	for in, want := range map[string]bool{
		"# " + pub1 + "\n":                      false,
		"   #" + pub1 + "\n":                    false,
		pub1 + "\n":                             true,
		"# note\nrestrict " + pub1 + "\n":       true,
		"ssh-ed25519 AAAAOTHER x\n":             false,
		"# " + pub1 + "\nssh-ed25519 OTHER x\n": false,
	} {
		if got := authorizedHas(in, blob); got != want {
			t.Errorf("%q: got %v", in, got)
		}
	}
}

func TestBreakGlassCheckIgnoresCommentedKey(t *testing.T) {
	needKeygen(t)
	w, d, _ := newWorld(t)
	addAcme(t, w, d)
	b := w.boxes["acme-dev"]
	var bg string
	for _, l := range strings.Split(b.authkeys, "\n") {
		if strings.HasPrefix(l, "ssh-ed25519") {
			bg = l
		}
	}
	if bg == "" {
		t.Fatalf("no break-glass key on the box: %q", b.authkeys)
	}
	b.authkeys = "# " + bg + "\n"
	steps := AddSteps(d, &Machine{Name: "acme-dev", Scope: "work/acme", Target: "t", User: "dev", Athome: "v0.4.0", Home: "/home/dev", Hostname: "dev"})
	for _, s := range steps {
		if s.Name != "machine-break-glass" {
			continue
		}
		ok, err := s.Check(t.Context(), nil)
		if err != nil || ok {
			t.Fatalf("a commented key counts as authorized: %v %v", ok, err)
		}
		return
	}
	t.Fatal("no break-glass step")
}

func TestAddLeftBehindNamesGitHubKeysWhenBoxKeyFailed(t *testing.T) {
	needKeygen(t)
	for _, fail := range []string{"gh-add-sign", "github-check"} {
		t.Run(fail, func(t *testing.T) {
			w, d, _ := newWorld(t)
			w.setFail(fail, true)
			err := Add(t.Context(), d, newAdd())
			if err == nil || !strings.HasPrefix(err.Error(), "athome machine add: machine-box-key: ") {
				t.Fatalf("err = %v", err)
			}
			if !strings.Contains(err.Error(), "left behind: pinned host key, GitHub keys athome-acme-dev[-sign]\n") {
				t.Errorf("left behind: %v", err)
			}
		})
	}
}
