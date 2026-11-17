package cli_test

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/cli"
	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/run/runtest"
)

// updateFixture: a repo, a fake runner that knows the tags, and a version.
func updateFixture(t *testing.T, version string, tags string) (repo string, f *runtest.Fake) {
	t.Helper()
	home := sandbox(t)
	_ = home
	repo = inRepo(t)
	f = runtest.NewFake()
	f.On("git", "-C", repo, "tag", "--list").Return(run.Result{Stdout: tags})
	f.On("git", "-C", repo, "ls-remote", "--tags", "--refs", "origin").Return(run.Result{Stdout: remoteTags(tags)})
	f.On("git", "-C", repo, "symbolic-ref", "--quiet", "--short", "HEAD").Return(run.Result{Stdout: "main\n"})
	f.On("mise", "ls", "--missing", "--json").Return(run.Result{Stdout: "{}"})
	t.Cleanup(cli.SetRunner(f))
	old := cli.Version
	cli.Version = version
	t.Cleanup(func() { cli.Version = old })
	t.Cleanup(cli.SetStdinTTY(false))
	t.Cleanup(cli.SetUpdateExec(func(string, []string) error { return nil }))
	return repo, f
}

// remoteTags renders a tag list as git ls-remote prints it.
func remoteTags(tags string) string {
	var b strings.Builder
	for _, t := range strings.Fields(tags) {
		fmt.Fprintf(&b, "abc123\trefs/tags/%s\n", t)
	}
	return b.String()
}

func called(f *runtest.Fake, name string, args ...string) bool {
	for _, c := range f.Calls() {
		if c.Name == name && reflect.DeepEqual(c.Args, args) {
			return true
		}
	}
	return false
}

func TestUpdateUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"update", "--to", "v0.5"},
		{"update", "--to", "latest"},
		{"update", "--bogus"},
		{"update", "extra"},
	} {
		repo, f := updateFixture(t, "0.5.0", "v0.5.0\n")
		_ = repo
		code, _, stderr := runMain(args...)
		if code != 2 || !strings.Contains(stderr, "athome update:") {
			t.Errorf("%v: code %d stderr %q", args, code, stderr)
		}
		if len(f.Calls()) != 0 {
			t.Errorf("%v: ran %v", args, f.Calls())
		}
	}
}

func TestUpdateHelp(t *testing.T) {
	updateFixture(t, "0.5.0", "")
	code, stdout, _ := runMain("update", "--help")
	if code != 0 || !strings.Contains(stdout, "--to") || !strings.Contains(stdout, "--yes") {
		t.Fatalf("code %d out %q", code, stdout)
	}
}

func TestUpdateNoRepo(t *testing.T) {
	sandbox(t)
	inNoRepo(t)
	code, _, stderr := runMain("update")
	if code != 1 || !strings.Contains(stderr, "athome repo not found") {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
}

func TestUpdateFetchFailureRunsNoStep(t *testing.T) {
	repo, f := updateFixture(t, "0.5.0", "v0.5.2\n")
	f.On("git", "-C", repo, "fetch", "--quiet", "--tags", "--force", "origin").Return(run.Result{Code: 128, Stderr: "no route"})
	code, _, stderr := runMain("update", "--yes")
	if code != 1 || !strings.Contains(stderr, "fetch") {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	if called(f, "chezmoi", "apply") || called(f, "git", "-C", repo, "pull", "--ff-only") {
		t.Errorf("a step ran: %v", f.Calls())
	}
}

func TestUpdateToMissingTag(t *testing.T) {
	_, f := updateFixture(t, "0.5.0", "v0.5.0\nv0.5.2\n")
	code, _, stderr := runMain("update", "--to", "9.9.9")
	if code != 1 || !strings.Contains(stderr, "v9.9.9") {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	if called(f, "chezmoi", "apply") {
		t.Error("apply ran")
	}
}

func TestUpdateToTagOnlyLocalExits1(t *testing.T) {
	repo, f := updateFixture(t, "0.5.0", "v0.5.0\nv0.5.2\n")
	f.On("git", "-C", repo, "ls-remote", "--tags", "--refs", "origin").Return(run.Result{Stdout: remoteTags("v0.5.0\n")})
	code, _, stderr := runMain("update", "--to", "0.5.2")
	if code != 1 || !strings.Contains(stderr, "origin has no tag v0.5.2") {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	if called(f, "chezmoi", "apply") {
		t.Error("apply ran")
	}
}

func TestUpdateNoTags(t *testing.T) {
	updateFixture(t, "0.5.0", "nightly\n")
	if code, _, stderr := runMain("update"); code != 1 || !strings.Contains(stderr, "no release tag") {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
}

func TestUpdateAlreadyCurrent(t *testing.T) {
	repo, f := updateFixture(t, "v0.5.2", "v0.5.0\nv0.5.2\nv0.5.10\n")
	// highest is 0.5.10; running 0.5.2 would update, so pin --to to the running one
	code, stdout, stderr := runMain("update", "--to", "0.5.2")
	if code != 0 {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	for _, want := range []string{"skip  self", "apply source", "apply apply", "skip  mise", "athome: applied 2, skipped 2"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	for _, c := range [][]string{{"git", "-C", repo, "pull", "--ff-only"}, {"chezmoi", "apply"}} {
		if !called(f, c[0], c[1:]...) {
			t.Errorf("missing %v", c)
		}
	}
	if !called(f, "git", "-C", repo, "fetch", "--quiet", "--tags", "--force", "origin") {
		t.Error("no fetch")
	}
}

func TestUpdateSelfThenExec(t *testing.T) {
	_, f := updateFixture(t, "0.5.0", "v0.5.0\nv0.5.2\n")
	body := "new athome"
	asset := fmt.Sprintf("athome_%s_%s", runtime.GOOS, runtime.GOARCH)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		switch q.URL.Path {
		case "/v0.5.2/" + asset:
			fmt.Fprint(w, body)
		case "/v0.5.2/checksums.txt":
			fmt.Fprintf(w, "%x  %s\n", sha256.Sum256([]byte(body)), asset)
		default:
			http.NotFound(w, q)
		}
	}))
	defer srv.Close()
	t.Setenv("ATHOME_RELEASE_BASE", srv.URL)
	var argv []string
	t.Cleanup(cli.SetUpdateExec(func(p string, a []string) error { argv = a; return nil }))
	code, stdout, stderr := runMain("update", "-y")
	if code != 0 {
		t.Fatalf("code %d stderr %q out %q", code, stderr, stdout)
	}
	home := os.Getenv("HOME")
	dest := filepath.Join(home, ".local", "bin", "athome")
	if b, _ := os.ReadFile(dest); string(b) != body {
		t.Fatalf("binary %q", b)
	}
	if !reflect.DeepEqual(argv, []string{dest, "update", "--to", "v0.5.2", "--yes"}) {
		t.Errorf("exec %v", argv)
	}
	_ = f
}

func TestUpdateStepFailureExits1(t *testing.T) {
	_, f := updateFixture(t, "0.5.2", "v0.5.2\n")
	f.On("chezmoi", "apply").Return(run.Result{Code: 1})
	code, _, stderr := runMain("update")
	if code != 1 || !strings.Contains(stderr, "step apply") {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
}

func TestUpdateCancelledAtPrompt(t *testing.T) {
	_, f := updateFixture(t, "0.5.0", "v0.5.2\n")
	t.Cleanup(cli.SetStdinTTY(true))
	in := filepath.Join(t.TempDir(), "in")
	os.WriteFile(in, []byte("n\n"), 0o644)
	fh, err := os.Open(in)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	old := os.Stdin
	os.Stdin = fh
	defer func() { os.Stdin = old }()
	code, stdout, stderr := runMain("update")
	if code != 0 || !strings.Contains(stdout, "athome update: cancelled, nothing changed") {
		t.Fatalf("code %d out %q err %q", code, stdout, stderr)
	}
	if called(f, "chezmoi", "apply") {
		t.Error("apply ran after cancel")
	}
}
