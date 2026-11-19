package machines

import (
	"slices"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/run/runtest"
)

func TestDeleteByTitleKeepsOtherMachinesKeys(t *testing.T) {
	w, d, _ := newWorld(t)
	w.ghKeys["user/keys"] = []fakeGHKey{
		{"1", "ssh-ed25519 AAAAA", "athome-foo"},
		{"2", "ssh-ed25519 AAAAB", "athome-foo-sign"}, // machine foo-sign's authentication key
	}
	w.ghKeys["user/ssh_signing_keys"] = []fakeGHKey{
		{"3", "ssh-ed25519 AAAAA", "athome-foo-sign"},
		{"4", "ssh-ed25519 AAAAB", "athome-foo-sign-sign"},
		{"5", "ssh-ed25519 AAAAC", "athome-foo"}, // not a title rm foo owns on this endpoint
	}
	n, err := d.gh(Machine{}).deleteByTitle(t.Context(), "foo")
	if err != nil || n != 2 {
		t.Fatalf("deleted %d: %v", n, err)
	}
	if got := w.ghKeys["user/keys"]; len(got) != 1 || got[0].id != "2" {
		t.Errorf("user/keys = %+v", got)
	}
	if got := w.ghKeys["user/ssh_signing_keys"]; len(got) != 2 || got[0].id != "4" || got[1].id != "5" {
		t.Errorf("user/ssh_signing_keys = %+v", got)
	}
}

func TestGHClientWithoutATokenSaysWhyAndAsksAgain(t *testing.T) {
	f := runtest.NewFake()
	f.On("gh", "auth", "token", "--hostname", "github.com", "--user", "octo").Return(run.Result{Code: 1, Stdout: "partial", Stderr: "no oauth token found for octo"})
	g := &ghClient{r: f, account: "octo"}
	for range 2 {
		_, err := g.run(t.Context(), "api", "user")
		if err == nil || err.Error() != "no gh token for account octo: no oauth token found for octo" {
			t.Fatalf("err = %v", err)
		}
	}
	var tokens int
	for _, c := range f.Calls() {
		if len(c.Args) > 1 && c.Args[0] == "auth" && c.Args[1] == "token" {
			tokens++
		}
		if len(c.Env) > 0 || slices.Equal(c.Args, []string{"api", "user"}) {
			t.Errorf("ran %v with env %v", c.Args, c.Env)
		}
	}
	if tokens != 2 {
		t.Errorf("token fetched %d times, want 2: a failed fetch is not cached", tokens)
	}
}
