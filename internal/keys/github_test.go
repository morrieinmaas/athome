package keys

import (
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/run/runtest"
)

// pubBlob is PubBlob, under the name these tests were written with.
func pubBlob(pub string) string { return PubBlob(pub) }

func TestGHKeysListsWithAndWithoutTitles(t *testing.T) {
	f := runtest.NewFake()
	f.On("gh", "api", "--paginate", "user/keys", "--jq", `.[] | "\(.id) \(.key)"`).Return(run.Result{Stdout: "1 ssh-ed25519 AAA\nshort line\n"})
	f.On("gh", "api", "--paginate", "user/keys", "--jq", `.[] | "\(.id) \(.key) \(.title)"`).Return(run.Result{Stdout: "1 ssh-ed25519 AAA my laptop\n"})
	ks, err := GHKeys(t.Context(), f, "user/keys", false)
	if err != nil || !slices.Equal(ks, []GHKey{{ID: "1", Blob: "AAA"}}) {
		t.Fatalf("without titles: %+v %v", ks, err)
	}
	ks, err = GHKeys(t.Context(), f, "user/keys", true)
	if err != nil || !slices.Equal(ks, []GHKey{{ID: "1", Blob: "AAA", Title: "my laptop"}}) {
		t.Fatalf("with titles: %+v %v", ks, err)
	}
}

func TestGHKeysAndDeleteReportGhFailures(t *testing.T) {
	f := runtest.NewFake()
	f.On("gh", "api", "--paginate", "user/keys", "--jq", `.[] | "\(.id) \(.key)"`).Return(run.Result{Code: 1, Stderr: "HTTP 401\n"})
	f.On("gh", "api", "-X", "DELETE", "user/keys/7").Return(run.Result{Code: 1, Stderr: "HTTP 404\n"})
	if _, err := GHKeys(t.Context(), f, "user/keys", false); err == nil || err.Error() != "gh api user/keys failed: HTTP 401" {
		t.Errorf("list: %v", err)
	}
	if err := GHDeleteKey(t.Context(), f, "user/keys", "7"); err == nil || err.Error() != "gh api -X DELETE user/keys/7 failed: HTTP 404" {
		t.Errorf("delete: %v", err)
	}
}

func TestWithPubFileHandsOverTheKeyAndRemovesIt(t *testing.T) {
	var path string
	err := WithPubFile("ssh-ed25519 AAA c", func(p string) error {
		path = p
		b, err := os.ReadFile(p)
		if err != nil || string(b) != "ssh-ed25519 AAA c\n" {
			t.Errorf("file: %q %v", b, err)
		}
		return errors.New("from fn")
	})
	if err == nil || err.Error() != "from fn" {
		t.Errorf("err = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("%s left behind: %v", path, err)
	}
}

func TestGHTokenRunner(t *testing.T) {
	f := runtest.NewFake()
	if r, err := GHTokenRunner(t.Context(), f, ""); err != nil || r != run.Runner(f) {
		t.Fatalf("no account: %v %v", r, err)
	}
	f.On("gh", "auth", "token", "--hostname", "github.com", "--user", "octo").Return(run.Result{Stdout: "tok\n"})
	r, err := GHTokenRunner(t.Context(), f, "octo")
	if err != nil {
		t.Fatal(err)
	}
	r.Run(t.Context(), run.Cmd{Name: "gh", Args: []string{"api", "user"}, Env: []string{"A=1"}})
	calls := f.Calls()
	if last := calls[len(calls)-1]; !slices.Equal(last.Env, []string{"A=1", "GH_TOKEN=tok"}) {
		t.Errorf("env = %v", last.Env)
	}

	f.On("gh", "auth", "token", "--hostname", "github.com", "--user", "nobody").Return(run.Result{Code: 1, Stderr: " no token \n"})
	_, err = GHTokenRunner(t.Context(), f, "nobody")
	var nt *NoTokenError
	if !errors.As(err, &nt) || nt.Account != "nobody" || nt.Stderr != "no token" {
		t.Fatalf("err = %#v", err)
	}
	if err.Error() != "gh has no token for GitHub account nobody (run: gh auth login)" {
		t.Errorf("message = %q", err.Error())
	}
}
