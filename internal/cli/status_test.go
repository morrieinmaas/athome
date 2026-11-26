package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/morrieinmaas/athome/internal/cli"
	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/run/runtest"
)

type statusEnv struct {
	home, repo string
	fake       *runtest.Fake
}

const canopyPin = "f445cd0f9f665d2ea3fb6e0694934ae45ef10942"

// newStatusEnv builds a temp home and repo where every component is current.
func newStatusEnv(t *testing.T) *statusEnv {
	t.Helper()
	home, repo := t.TempDir(), t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(repo, ".chezmoiroot"), []byte("home\n"), 0o644))
	must(os.MkdirAll(filepath.Join(repo, "home", ".chezmoidata"), 0o755))
	must(os.WriteFile(filepath.Join(repo, "home", ".chezmoidata", "pins.toml"),
		[]byte("[pins]\ngrofe = \"v0.3.1\"\ncanopy = \""+canopyPin+"\"\n"), 0o644))
	grofe := filepath.Join(home, ".local", "lib", "grofe", "grofe")
	must(os.MkdirAll(filepath.Dir(grofe), 0o755))
	must(os.WriteFile(grofe, []byte("x"), 0o755))
	must(os.MkdirAll(filepath.Join(home, ".local", "share", "canopy"), 0o755))
	t.Setenv("HOME", home)
	t.Setenv("ATHOME_REPO", repo)
	old := cli.Version
	cli.Version = "0.5.2"
	t.Cleanup(func() { cli.Version = old })

	canopy := filepath.Join(home, ".local", "share", "canopy")
	f := runtest.NewFake()
	f.Have("chezmoi", "mise")
	ok := func(out string) run.Result { return run.Result{Stdout: out} }
	f.On("git", "-C", repo, "ls-remote", "--tags", "--refs", "origin").
		Return(ok("aaa\trefs/tags/v0.5.1\nbbb\trefs/tags/v0.5.2\nccc\trefs/tags/v0.5.10-rc\nddd\trefs/tags/v0.4.9\n"))
	f.On("git", "-C", repo, "symbolic-ref", "--short", "-q", "HEAD").Return(ok("main\n"))
	f.On("chezmoi", "status", "--exclude=scripts").Return(ok(""))
	f.On("mise", "ls", "--missing", "--json").Return(ok("{}\n"))
	f.On(grofe, "-V").Return(ok("grofe 0.3.1\n"))
	f.On("git", "-C", canopy, "rev-parse", "HEAD").Return(ok(canopyPin + "\n"))
	f.On("git", "-C", canopy, "rev-parse", "--verify", canopyPin+"^{commit}").Return(ok(canopyPin + "\n"))
	t.Cleanup(cli.SetRunner(f))
	return &statusEnv{home: home, repo: repo, fake: f}
}

func runStatus(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := cli.Main(append([]string{"status"}, args...), &out, &errb)
	return code, out.String(), errb.String()
}

type jsonReport struct {
	Version    string `json:"version"`
	Commit     string `json:"commit"`
	Repo       string `json:"repo"`
	Ref        string `json:"ref"`
	Current    bool   `json:"current"`
	Components []struct {
		Name      string `json:"name"`
		Status    string `json:"status"`
		Installed string `json:"installed"`
		Wanted    string `json:"wanted"`
		Detail    string `json:"detail"`
	} `json:"components"`
}

func statusJSON(t *testing.T) jsonReport {
	t.Helper()
	code, out, errs := runStatus("--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	var r jsonReport
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("bad json %q: %v", out, err)
	}
	return r
}

func statusOf(r jsonReport, name string) string {
	for _, c := range r.Components {
		if c.Name == name {
			return c.Status
		}
	}
	return "?"
}

func TestStatusAllCurrent(t *testing.T) {
	e := newStatusEnv(t)
	code, out, _ := runStatus()
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 6 {
		t.Fatalf("want header + 5 rows, got %q", out)
	}
	if want := "athome 0.5.2 (" + cli.Commit + "), repo " + e.repo + " at main"; lines[0] != want {
		t.Errorf("header %q, want %q", lines[0], want)
	}
	rows := []string{
		"athome    current        0.5.2",
		"dotfiles  current        ",
		"mise      current        ",
		"grofe     current        0.3.1",
		"canopy    current        " + canopyPin,
	}
	for i, want := range rows {
		if lines[i+1] != want {
			t.Errorf("row %d = %q, want %q", i, lines[i+1], want)
		}
	}
	r := statusJSON(t)
	if !r.Current || len(r.Components) != 5 || r.Ref != "main" || r.Repo != e.repo {
		t.Errorf("report %+v", r)
	}
	for i, name := range []string{"athome", "dotfiles", "mise", "grofe", "canopy"} {
		if r.Components[i].Name != name || r.Components[i].Status != "current" {
			t.Errorf("component %d = %+v", i, r.Components[i])
		}
	}
}

func TestStatusAthomeBehindAndOffline(t *testing.T) {
	e := newStatusEnv(t)
	cli.Version = "0.5.1"
	r := statusJSON(t)
	c := r.Components[0]
	if c.Status != "not current" || c.Installed != "0.5.1" || c.Wanted != "v0.5.2" || r.Current {
		t.Errorf("behind: %+v current=%v", c, r.Current)
	}
	e.fake.On("git", "-C", e.repo, "ls-remote", "--tags", "--refs", "origin").Return(run.Result{Code: 128})
	if s := statusOf(statusJSON(t), "athome"); s != "unknown" {
		t.Errorf("offline athome = %q", s)
	}
}

func TestStatusDotfilesAndMise(t *testing.T) {
	e := newStatusEnv(t)
	e.fake.On("chezmoi", "status", "--exclude=scripts").Return(run.Result{Stdout: " M .zshrc\n M .gitconfig\n"})
	e.fake.On("mise", "ls", "--missing", "--json").Return(run.Result{Stdout: `{"node": [{"version": "22"}]}`})
	r := statusJSON(t)
	if statusOf(r, "dotfiles") != "not current" || statusOf(r, "mise") != "not current" || r.Current {
		t.Errorf("report %+v", r)
	}
}

func TestStatusGrofe(t *testing.T) {
	e := newStatusEnv(t)
	grofe := filepath.Join(e.home, ".local", "lib", "grofe", "grofe")
	e.fake.On(grofe, "-V").Return(run.Result{Stdout: "grofe 0.3.0\n"})
	if s := statusOf(statusJSON(t), "grofe"); s != "not current" {
		t.Errorf("old grofe = %q", s)
	}
	e.fake.On(grofe, "-V").Return(run.Result{Stdout: "grofe dev\n"})
	if s := statusOf(statusJSON(t), "grofe"); s != "unknown" {
		t.Errorf("dev grofe = %q", s)
	}
	os.Remove(grofe)
	if s := statusOf(statusJSON(t), "grofe"); s != "not installed" {
		t.Errorf("missing grofe = %q", s)
	}
}

func TestStatusCanopy(t *testing.T) {
	e := newStatusEnv(t)
	store := filepath.Join(e.home, ".local", "share", "canopy")
	e.fake.On("git", "-C", store, "rev-parse", "--verify", canopyPin+"^{commit}").Return(run.Result{Code: 128})
	r := statusJSON(t)
	if statusOf(r, "canopy") != "not current" {
		t.Errorf("unfetched pin = %q", statusOf(r, "canopy"))
	}
	if d := r.Components[4].Detail; !strings.Contains(d, canopyPin) {
		t.Errorf("detail %q does not name the pin", d)
	}
	e.fake.On("git", "-C", store, "rev-parse", "--verify", canopyPin+"^{commit}").Return(run.Result{Stdout: "0123456789012345678901234567890123456789\n"})
	if s := statusOf(statusJSON(t), "canopy"); s != "not current" {
		t.Errorf("other head = %q", s)
	}
	os.RemoveAll(store)
	if s := statusOf(statusJSON(t), "canopy"); s != "not installed" {
		t.Errorf("missing store = %q", s)
	}
}

func TestStatusNoPins(t *testing.T) {
	e := newStatusEnv(t)
	os.Remove(filepath.Join(e.repo, "home", ".chezmoidata", "pins.toml"))
	r := statusJSON(t)
	if statusOf(r, "grofe") != "unknown" || statusOf(r, "canopy") != "unknown" || r.Current {
		t.Errorf("report %+v", r)
	}
}

func TestStatusJSONKeys(t *testing.T) {
	newStatusEnv(t)
	_, out, _ := runStatus("--json")
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"version", "commit", "repo", "ref", "current", "components"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("missing key %q", k)
		}
	}
	var comps []map[string]json.RawMessage
	json.Unmarshal(raw["components"], &comps)
	for _, k := range []string{"name", "status", "installed", "wanted", "detail"} {
		if _, ok := comps[0][k]; !ok {
			t.Errorf("component missing key %q", k)
		}
	}
}

func TestStatusRefFallbacks(t *testing.T) {
	e := newStatusEnv(t)
	e.fake.On("git", "-C", e.repo, "symbolic-ref", "--short", "-q", "HEAD").Return(run.Result{Code: 1})
	e.fake.On("git", "-C", e.repo, "describe", "--tags", "--exact-match").Return(run.Result{Stdout: "v0.5.2\n"})
	if r := statusJSON(t); r.Ref != "v0.5.2" {
		t.Errorf("tag ref = %q", r.Ref)
	}
	e.fake.On("git", "-C", e.repo, "describe", "--tags", "--exact-match").Return(run.Result{Code: 128})
	e.fake.On("git", "-C", e.repo, "rev-parse", "--short", "HEAD").Return(run.Result{Stdout: "abc1234\n"})
	if r := statusJSON(t); r.Ref != "abc1234" {
		t.Errorf("commit ref = %q", r.Ref)
	}
}

func TestStatusUsageAndMissingRepo(t *testing.T) {
	newStatusEnv(t)
	if code, _, _ := runStatus("--bogus"); code != 2 {
		t.Errorf("--bogus exit %d", code)
	}
	if code, _, _ := runStatus("extra"); code != 2 {
		t.Errorf("extra arg exit %d", code)
	}
	t.Setenv("ATHOME_REPO", t.TempDir())
	if code, _, errs := runStatus(); code != 1 || !strings.Contains(errs, "athome status:") {
		t.Errorf("missing repo exit %d, %q", code, errs)
	}
}

func TestStatusKeysReminder(t *testing.T) {
	e := newStatusEnv(t)
	_, out, _ := runStatus()
	if strings.Contains(out, "keys:") {
		t.Errorf("reminder with no laptop key:\n%s", out)
	}
	key := keysLaptopKey(e.home)
	os.MkdirAll(filepath.Dir(key), 0o700)
	os.WriteFile(key, []byte("k"), 0o600)
	old := time.Now().Add(-120 * 24 * time.Hour)
	os.Chtimes(key, old, old)
	code, out, _ := runStatus()
	if code != 0 || !strings.Contains(out, "keys: last rotated 120 days ago; run athome keys rotate --laptop") {
		t.Errorf("code %d, out:\n%s", code, out)
	}
	_, js, _ := runStatus("--json")
	if !strings.Contains(js, `"due":true`) || !strings.Contains(js, `"current":true`) {
		t.Errorf("json = %s", js)
	}
}

func TestStatusQuiet(t *testing.T) {
	e := newStatusEnv(t)
	if code, out, errs := runStatus("--quiet"); code != 0 || out != "" || errs != "" {
		t.Errorf("all current: exit %d, out %q, err %q", code, out, errs)
	}

	grofe := filepath.Join(e.home, ".local", "lib", "grofe", "grofe")
	e.fake.On(grofe, "-V").Return(run.Result{Stdout: "grofe 0.3.0\n"})
	os.RemoveAll(filepath.Join(e.home, ".local", "share", "canopy"))
	if code, out, _ := runStatus("--quiet"); code != 3 || out != "grofe: chezmoi apply\ncanopy: chezmoi apply\n" {
		t.Errorf("not current and not installed: exit %d, out %q", code, out)
	}

	e.fake.On("git", "-C", e.repo, "ls-remote", "--tags", "--refs", "origin").Return(run.Result{Code: 128})
	if code, out, _ := runStatus("--quiet"); code != 3 || out != "grofe: chezmoi apply\ncanopy: chezmoi apply\n" {
		t.Errorf("offline is not drift: exit %d, out %q", code, out)
	}

	if code, _, _ := runStatus("--quiet", "--json"); code != 2 {
		t.Errorf("--quiet with --json exit %d, want 2", code)
	}
	t.Setenv("ATHOME_REPO", t.TempDir())
	if code, out, _ := runStatus("--quiet"); code != 1 || out != "" {
		t.Errorf("missing repo: exit %d, out %q", code, out)
	}
}

// dotfiles and mise are not drift for --quiet (chezmoi apply would overwrite
// local edits), so --quiet does not even run their checks.
func TestStatusQuietSkipsDotfilesAndMise(t *testing.T) {
	e := newStatusEnv(t)
	e.fake.On("chezmoi", "status", "--exclude=scripts").Return(run.Result{Stdout: " M .zshrc\n"})
	e.fake.On("mise", "ls", "--missing", "--json").Return(run.Result{Stdout: `{"node": [{"version": "22"}]}`})
	if code, out, _ := runStatus("--quiet"); code != 0 || out != "" {
		t.Errorf("dotfiles and mise behind: exit %d, out %q", code, out)
	}
	for _, c := range e.fake.Calls() {
		if c.Name == "chezmoi" || c.Name == "mise" {
			t.Errorf("--quiet ran %s %v", c.Name, c.Args)
		}
	}
}

// What --quiet cannot judge fails the check (exit 1, the reason on stderr), so a
// broken check never reads as all current.
func TestStatusQuietUnknownFails(t *testing.T) {
	pins := func(e *statusEnv) string { return filepath.Join(e.repo, "home", ".chezmoidata", "pins.toml") }
	cases := map[string]func(e *statusEnv){
		"corrupt pins.toml": func(e *statusEnv) { os.WriteFile(pins(e), []byte("[pins\n"), 0o644) },
		"missing pins.toml": func(e *statusEnv) { os.Remove(pins(e)) },
		"canopy HEAD unreadable": func(e *statusEnv) {
			store := filepath.Join(e.home, ".local", "share", "canopy")
			e.fake.On("git", "-C", store, "rev-parse", "HEAD").Return(run.Result{Code: 128})
		},
		"grofe -V fails": func(e *statusEnv) {
			e.fake.On(filepath.Join(e.home, ".local", "lib", "grofe", "grofe"), "-V").Return(run.Result{Code: 1})
		},
		"athome dev build": func(e *statusEnv) { cli.Version = "dev" },
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			e := newStatusEnv(t)
			breakIt(e)
			if code, out, errs := runStatus("--quiet"); code != 1 || out != "" || !strings.HasPrefix(errs, "athome status: ") {
				t.Errorf("exit %d, out %q, err %q", code, out, errs)
			}
		})
	}
}

// athome below its pin is fixed by chezmoi apply; at or above it but behind the
// latest tag, by athome update.
func TestStatusQuietAthomeFix(t *testing.T) {
	e := newStatusEnv(t)
	cli.Version = "0.5.1"
	if code, out, _ := runStatus("--quiet"); code != 3 || out != "athome: athome update\n" {
		t.Errorf("behind the latest tag: exit %d, out %q", code, out)
	}
	pins := filepath.Join(e.repo, "home", ".chezmoidata", "pins.toml")
	os.WriteFile(pins, []byte("[pins]\nathome = \"v0.5.2\"\ngrofe = \"v0.3.1\"\ncanopy = \""+canopyPin+"\"\n"), 0o644)
	if code, out, _ := runStatus("--quiet"); code != 3 || out != "athome: chezmoi apply\n" {
		t.Errorf("below the pin: exit %d, out %q", code, out)
	}
}
