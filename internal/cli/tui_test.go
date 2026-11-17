package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/morrieinmaas/athome/internal/prompt"
	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/run/runtest"
	"github.com/morrieinmaas/athome/internal/steps"
	"github.com/morrieinmaas/athome/internal/tui"
)

// tuiSandbox gives a fresh HOME and repo, a terminal, and fake screens; it returns
// the home and the list of screens that were shown.
func tuiSandbox(t *testing.T) (home string, shown *[]string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("PATH", os.Getenv("PATH"))
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("HOSTNAME", "testbox")
	for _, k := range []string{"GITHUB_TOKEN", "ATHOME_CI", "ATHOME_REPO", "ATHOME_PLAIN", "ATHOME_GITHUB_HANDLE", "ATHOME_WORK_EMAIL"} {
		t.Setenv(k, "")
	}
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, ".chezmoiroot"), []byte("home\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)

	oldIn, oldOut, oldForm, oldProg, oldHome, oldList, oldRunner := stdinIsTTY, stdoutIsTTY, runForm, runProgress, runHome, stepList, newRunner
	t.Cleanup(func() {
		stdinIsTTY, stdoutIsTTY, runForm, runProgress, runHome, stepList, newRunner = oldIn, oldOut, oldForm, oldProg, oldHome, oldList, oldRunner
	})
	stdinIsTTY, stdoutIsTTY = func() bool { return true }, func() bool { return true }
	newRunner = func() run.Runner { return runtest.NewFake() }
	stepList = steps.Data
	shown = &[]string{}
	runForm = func(context.Context, io.Reader, io.Writer, tui.Seed) (map[string]string, error) {
		*shown = append(*shown, "form")
		return nil, tui.ErrQuit
	}
	runProgress = func(ctx context.Context, _ io.Reader, _ io.Writer, env *steps.Env, list []steps.Step, wire func(func(func()))) (steps.Report, error) {
		*shown = append(*shown, "progress")
		wire(func(fn func()) { fn() })
		return steps.Execute(ctx, env, list)
	}
	return home, shown
}

func run2(args ...string) (int, string, string) {
	var o, e strings.Builder
	code := Main(args, &o, &e)
	return code, o.String(), e.String()
}

func TestUseTUISelection(t *testing.T) {
	for _, c := range []struct {
		name    string
		o       initOpts
		env     map[string]string
		in, out bool
		wantTUI bool
	}{
		{"terminal", initOpts{}, nil, true, true, true},
		{"plain flag", initOpts{Plain: true}, nil, true, true, false},
		{"ATHOME_PLAIN", initOpts{}, map[string]string{"ATHOME_PLAIN": "1"}, true, true, false},
		{"ATHOME_PLAIN=0", initOpts{}, map[string]string{"ATHOME_PLAIN": "0"}, true, true, true},
		{"-y", initOpts{Yes: true}, nil, true, true, false},
		{"non-interactive", initOpts{NonInter: true}, nil, true, true, false},
		{"stdin pipe", initOpts{}, nil, false, true, false},
		{"stdout pipe", initOpts{}, nil, true, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			oldIn, oldOut := stdinIsTTY, stdoutIsTTY
			t.Cleanup(func() { stdinIsTTY, stdoutIsTTY = oldIn, oldOut })
			stdinIsTTY, stdoutIsTTY = func() bool { return c.in }, func() bool { return c.out }
			if got := useTUI(c.o, c.env); got != c.wantTUI {
				t.Errorf("useTUI = %v, want %v", got, c.wantTUI)
			}
		})
	}
}

func TestInitRoutesByFlags(t *testing.T) {
	for _, c := range []struct {
		name string
		args []string
		env  string
		want []string
	}{
		{"tui", []string{"init"}, "", []string{"form"}},
		{"plain", []string{"init", "--plain"}, "", nil},
		{"env plain", []string{"init"}, "1", nil},
		{"yes", []string{"init", "-y"}, "", nil},
		{"config file", []string{"init", "--config", "CFG"}, "", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, shown := tuiSandbox(t)
			t.Setenv("ATHOME_PLAIN", c.env)
			args := append([]string{}, c.args...)
			for i, a := range args {
				if a == "CFG" {
					args[i] = filepath.Join(t.TempDir(), "a.toml")
					os.WriteFile(args[i], []byte("personalName = \"x\"\n"), 0o644)
				}
			}
			stdinIsTTY = func() bool { return true }
			if len(c.want) == 0 { // plain paths must not read the real stdin: no tty answers
				stdinIsTTY = func() bool { return c.name == "plain" || c.name == "env plain" || c.name == "config file" }
			}
			stepList = func() []steps.Step { return nil }
			run2(args...)
			if !reflect.DeepEqual(*shown, c.want) && !(len(*shown) == 0 && len(c.want) == 0) {
				t.Errorf("screens = %v, want %v", *shown, c.want)
			}
		})
	}
}

func TestQuitExits130AndWritesNothing(t *testing.T) {
	home, shown := tuiSandbox(t)
	code, _, _ := run2("init")
	if code != 130 {
		t.Errorf("exit = %d, want 130", code)
	}
	if !reflect.DeepEqual(*shown, []string{"form"}) {
		t.Errorf("screens = %v; progress must not run after a quit", *shown)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "chezmoi", "chezmoi.toml")); !os.IsNotExist(err) {
		t.Errorf("chezmoi.toml must not exist after a quit, stat err = %v", err)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Acceptance criterion 2: form answers and the same values in a --config run give one chezmoi.toml.
func TestFormAnswersWriteSameDataAsConfigRun(t *testing.T) {
	home, _ := tuiSandbox(t)
	runForm = func(context.Context, io.Reader, io.Writer, tui.Seed) (map[string]string, error) {
		return map[string]string{
			"machineType": "work", "hostname": "testbox", "includeAgents": "false",
			"githubHandle": "octo", "githubId": "5", "personalName": "Octo Cat",
			"workEmail": "a@b.co", "bitwardenUrl": "https://bw.example", "meshProvider": "netbird",
			"netbirdManagementUrl": "https://nb.example", "extraPackages": "jq",
			"useBitwarden": "true", "generateSshKey": "true", "uploadSshKeys": "true", "restoreSecrets": "true",
		}, nil
	}
	code, _, stderr := run2("init")
	if code != 0 {
		t.Fatalf("tui run exit %d: %s", code, stderr)
	}
	toml := filepath.Join(home, ".config", "chezmoi", "chezmoi.toml")
	viaForm := readFile(t, toml)
	os.Remove(toml)

	cfg := filepath.Join(t.TempDir(), "bootstrap.toml")
	os.WriteFile(cfg, []byte(`machine = "work"
includeAgents = false
githubHandle = "octo"
githubId = "5"
personalName = "Octo Cat"
workEmail = "a@b.co"
bitwardenUrl = "https://bw.example"
meshProvider = "netbird"
netbirdManagementUrl = "https://nb.example"
extraPackages = "jq"
`), 0o644)
	if code, _, stderr = run2("init", "--config", cfg, "-y"); code != 0 {
		t.Fatalf("config run exit %d: %s", code, stderr)
	}
	if viaConfig := readFile(t, toml); viaForm != viaConfig {
		t.Errorf("chezmoi.toml differs\nform:\n%s\nconfig:\n%s", viaForm, viaConfig)
	}
}

func TestMergeAnswers(t *testing.T) {
	env := &steps.Env{Cfg: steps.Env{}.Cfg}
	env.Cfg.Data = map[string]string{"sshKeyAction": "reupload", "workName": "x"}
	mergeAnswers(env, map[string]string{"machineType": "work", "includeAgents": "false", "generateSshKey": "false", "workName": "y"})
	if env.Cfg.Machine != "work" || env.Cfg.IncludeAgents {
		t.Errorf("machine/agents not mapped: %+v", env.Cfg)
	}
	d := env.Cfg.Data
	if d["workName"] != "y" || d["generateSshKey"] != "false" || d["sshKeyAction"] != "" {
		t.Errorf("data = %v", d)
	}
	if _, ok := d["machineType"]; ok {
		t.Error("machineType belongs outside Data")
	}
}

func TestProgressWireSuspendsAndAsks(t *testing.T) {
	_, _ = tuiSandbox(t)
	runForm = func(context.Context, io.Reader, io.Writer, tui.Seed) (map[string]string, error) {
		return map[string]string{"machineType": "personal", "includeAgents": "true"}, nil
	}
	suspended := 0
	runProgress = func(ctx context.Context, _ io.Reader, _ io.Writer, env *steps.Env, _ []steps.Step, wire func(func(func()))) (steps.Report, error) {
		wire(func(fn func()) { suspended++; fn() })
		if env.Ask == nil || env.Ask.Around == nil {
			t.Error("Ask must stay set with Around wired")
		}
		env.Run.Run(ctx, run.Cmd{Name: "bw-setup", Interactive: true})
		env.Run.Run(ctx, run.Cmd{Name: "git"})
		return steps.Report{}, nil
	}
	if code, _, e := run2("init"); code != 0 {
		t.Fatalf("exit %d: %s", code, e)
	}
	if suspended != 1 {
		t.Errorf("only the interactive command suspends: got %d", suspended)
	}
}

func TestProgressFailureAndCancel(t *testing.T) {
	_, _ = tuiSandbox(t)
	runForm = func(context.Context, io.Reader, io.Writer, tui.Seed) (map[string]string, error) {
		return map[string]string{"machineType": "personal"}, nil
	}
	runProgress = func(context.Context, io.Reader, io.Writer, *steps.Env, []steps.Step, func(func(func()))) (steps.Report, error) {
		return steps.Report{}, errors.New("boom")
	}
	code, _, stderr := run2("init")
	if code != 1 || !strings.Contains(stderr, "athome init: boom") {
		t.Errorf("failure: exit %d stderr %q", code, stderr)
	}
	runProgress = func(context.Context, io.Reader, io.Writer, *steps.Env, []steps.Step, func(func(func()))) (steps.Report, error) {
		return steps.Report{}, errors.Join(errors.New("run canceled"), context.Canceled)
	}
	if code, _, _ := run2("init"); code != 130 {
		t.Errorf("cancel: exit %d, want 130", code)
	}
}

func TestHomeRouting(t *testing.T) {
	for _, c := range []struct {
		choice   string
		wantCode int
		wantForm bool
	}{{"quit", 0, false}, {"init", 130, true}} {
		t.Run(c.choice, func(t *testing.T) {
			home, shown := tuiSandbox(t)
			chez := filepath.Join(home, ".config", "chezmoi")
			os.MkdirAll(chez, 0o755)
			os.WriteFile(filepath.Join(chez, "chezmoi.toml"), []byte("[data]\n  machineType = \"work\"\n  hostname = \"hb\"\n  githubHandle = \"octo\"\n"), 0o644)
			var info tui.HomeInfo
			runHome = func(_ io.Reader, _ io.Writer, i tui.HomeInfo) (string, error) { info = i; return c.choice, nil }
			code, out, _ := run2()
			if code != c.wantCode || (len(*shown) > 0) != c.wantForm {
				t.Errorf("exit %d screens %v", code, *shown)
			}
			if out != "" && strings.Contains(out, "Usage") {
				t.Error("a terminal must get the home screen, not usage")
			}
			if !info.SetUp || info.MachineType != "work" || info.Hostname != "hb" || info.Handle != "octo" || info.Version != "dev" || info.Repo == "" {
				t.Errorf("info = %+v", info)
			}
		})
	}
}

func TestNoArgsWithoutTerminalPrintsUsage(t *testing.T) {
	_, _ = tuiSandbox(t)
	stdoutIsTTY = func() bool { return false }
	runHome = func(io.Reader, io.Writer, tui.HomeInfo) (string, error) {
		t.Error("home screen must not run without a terminal")
		return "quit", nil
	}
	if code, out, _ := run2(); code != 0 || !strings.Contains(out, "Usage: athome") {
		t.Errorf("exit %d out %q", code, out)
	}
}

func TestHomeNotSetUpAndNoRepo(t *testing.T) {
	_, _ = tuiSandbox(t)
	t.Chdir(t.TempDir())
	var info tui.HomeInfo
	runHome = func(_ io.Reader, _ io.Writer, i tui.HomeInfo) (string, error) { info = i; return "quit", nil }
	run2()
	if info.SetUp || info.Repo != "" {
		t.Errorf("info = %+v, want not set up and empty repo", info)
	}
}

func TestCheckURLAndHandle(t *testing.T) {
	oldGet := httpGet
	t.Cleanup(func() { httpGet = oldGet })
	httpGet = func(context.Context, string) (int, error) { return 200, nil }
	if err := checkURL(context.Background(), "http://x"); err == nil || errors.Is(err, tui.ErrCheckUnavailable) {
		t.Errorf("http must be rejected for good: %v", err)
	}
	if err := checkURL(context.Background(), "https://x"); err != nil {
		t.Error(err)
	}
	httpGet = func(context.Context, string) (int, error) { return 0, errors.New("no route") }
	if err := checkURL(context.Background(), "https://x"); !errors.Is(err, tui.ErrCheckUnavailable) {
		t.Errorf("a transport error is overridable: %v", err)
	}
	f := runtest.NewFake()
	httpGet = func(context.Context, string) (int, error) { return 404, nil }
	if err := checkHandle(context.Background(), f, "nobody"); err == nil || errors.Is(err, tui.ErrCheckUnavailable) {
		t.Errorf("404 must block for good: %v", err)
	}
	httpGet = func(context.Context, string) (int, error) { return 200, nil }
	if err := checkHandle(context.Background(), f, "octo"); err != nil {
		t.Error(err)
	}
}

func TestCheckHandleClassifiesFailures(t *testing.T) {
	oldGet := httpGet
	t.Cleanup(func() { httpGet = oldGet })
	var gotURL string
	httpGet = func(_ context.Context, u string) (int, error) { gotURL = u; return 200, nil }
	f := runtest.NewFake()
	for _, h := range []string{"bad name", "-lead", "trail-", "dou--ble", "has/slash", "a?b", strings.Repeat("a", 40), ""} {
		gotURL = ""
		err := checkHandle(context.Background(), f, h)
		if err == nil || errors.Is(err, tui.ErrCheckUnavailable) {
			t.Errorf("handle %q must be rejected for good: %v", h, err)
		}
		if gotURL != "" {
			t.Errorf("handle %q reached the network: %s", h, gotURL)
		}
	}
	for _, h := range []string{"octo", "octo-cat", "a", strings.Repeat("a", 39)} {
		if err := checkHandle(context.Background(), f, h); err != nil {
			t.Errorf("handle %q: %v", h, err)
		}
	}
	if gotURL != "https://api.github.com/users/"+strings.Repeat("a", 39) {
		t.Errorf("url = %q", gotURL)
	}
	httpGet = func(context.Context, string) (int, error) { return 0, errors.New("offline") }
	if err := checkHandle(context.Background(), f, "octo"); !errors.Is(err, tui.ErrCheckUnavailable) {
		t.Errorf("a transport error is overridable: %v", err)
	}
	httpGet = func(context.Context, string) (int, error) { return 503, nil }
	if err := checkHandle(context.Background(), f, "octo"); !errors.Is(err, tui.ErrCheckUnavailable) {
		t.Errorf("an unexpected status is overridable: %v", err)
	}
}

// lockedBuf is an io.Writer the test can poll while the form writes to it.
type lockedBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// Pressing q on the real form's review page quits: exit 130 and no chezmoi.toml.
func TestQuitOnReviewPageExits130AndWritesNothing(t *testing.T) {
	home, shown := tuiSandbox(t)
	t.Setenv("ATHOME_GITHUB_HANDLE", "octo") // a required field the test need not type
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	defer pw.Close()
	var out lockedBuf
	var formErr error
	runForm = func(ctx context.Context, _ io.Reader, _ io.Writer, s tui.Seed) (map[string]string, error) {
		var ans map[string]string
		ans, formErr = tui.RunForm(ctx, pr, &out, s)
		return ans, formErr
	}
	runProgress = func(context.Context, io.Reader, io.Writer, *steps.Env, []steps.Step, func(func(func()))) (steps.Report, error) {
		*shown = append(*shown, "progress")
		return steps.Report{}, nil
	}
	exit := make(chan int, 1)
	go func() { code, _, _ := run2("init"); exit <- code }()

	waitFor := func(want string) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); !strings.Contains(out.String(), want); time.Sleep(10 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %q in %q", want, out.String())
			}
		}
	}
	// Each page ends with one enter per field; the pages are five, then the review.
	for _, page := range []struct {
		title  string
		fields int
	}{{"Machine", 2}, {"Identity", 6}, {"Keys and secrets", 5}, {"Network", 2}, {"Extras", 7}} {
		waitFor(page.title)
		pw.WriteString(strings.Repeat("\r", page.fields))
	}
	waitFor("enter apply, b back to edit, q quit")
	pw.WriteString("q")

	select {
	case code := <-exit:
		if code != 130 {
			t.Errorf("exit = %d, want 130", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("init did not exit after q on the review page")
	}
	if !errors.Is(formErr, tui.ErrQuit) {
		t.Errorf("RunForm err = %v, want ErrQuit", formErr)
	}
	if len(*shown) != 0 {
		t.Errorf("screens after quit = %v", *shown)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "chezmoi", "chezmoi.toml")); !os.IsNotExist(err) {
		t.Errorf("chezmoi.toml must not exist after a quit, stat err = %v", err)
	}
}

// The form's empty bitwardenUrl (US cloud) is an answer: the data step must not
// fall back to its line prompt for it.
func TestFormEmptyBitwardenUrlIsNotAskedAgain(t *testing.T) {
	home, _ := tuiSandbox(t)
	runForm = func(context.Context, io.Reader, io.Writer, tui.Seed) (map[string]string, error) {
		return map[string]string{
			"machineType": "personal", "includeAgents": "true", "githubHandle": "octo", "githubId": "5",
			"useBitwarden": "true", "bitwardenUrl": "", "generateSshKey": "false",
		}, nil
	}
	var asked bool
	runProgress = func(ctx context.Context, _ io.Reader, _ io.Writer, env *steps.Env, list []steps.Step, _ func(func(func()))) (steps.Report, error) {
		env.Ask = prompt.New(failReader{&asked}, io.Discard)
		return steps.Execute(ctx, env, list)
	}
	if code, _, e := run2("init"); code != 0 {
		t.Fatalf("exit %d: %s", code, e)
	}
	if asked {
		t.Error("the data step read an answer although the form answered the Bitwarden URL")
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "chezmoi", "chezmoi.toml")); err != nil {
		t.Errorf("chezmoi.toml not written: %v", err)
	}
}

type failReader struct{ asked *bool }

func (f failReader) Read([]byte) (int, error) { *f.asked = true; return 0, io.EOF }

// An explicit hostname answer is carried in the config, not in the process env.
func TestMergeAnswersHostnameNeedsNoProcessEnv(t *testing.T) {
	_, _ = tuiSandbox(t)
	t.Setenv("HOSTNAME", "testbox")
	env := &steps.Env{}
	mergeAnswers(env, map[string]string{"hostname": "other"})
	if got := env.Cfg.Data["hostname"]; got != "other" {
		t.Errorf("Data[hostname] = %q, want other", got)
	}
	if os.Getenv("HOSTNAME") != "testbox" {
		t.Errorf("HOSTNAME was changed to %q", os.Getenv("HOSTNAME"))
	}
	env = &steps.Env{}
	mergeAnswers(env, map[string]string{"hostname": "testbox"})
	if _, ok := env.Cfg.Data["hostname"]; ok {
		t.Error("the default hostname must not be pinned")
	}
}

// Ctrl-C in the progress view still runs the registered cleanups, and exits 130.
func TestProgressCancelRunsCleanup(t *testing.T) {
	_, _ = tuiSandbox(t)
	runForm = func(context.Context, io.Reader, io.Writer, tui.Seed) (map[string]string, error) {
		return map[string]string{"machineType": "personal"}, nil
	}
	cleaned := false
	runProgress = func(_ context.Context, _ io.Reader, _ io.Writer, env *steps.Env, _ []steps.Step, _ func(func(func()))) (steps.Report, error) {
		env.Cleanup = append(env.Cleanup, func() { cleaned = true })
		return steps.Report{}, errors.Join(errors.New("run canceled"), context.Canceled)
	}
	if code, _, _ := run2("init"); code != 130 {
		t.Errorf("exit %d, want 130", code)
	}
	if !cleaned {
		t.Error("env.Cleanup did not run after a cancel")
	}
}
