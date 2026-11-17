package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/config"
	"github.com/morrieinmaas/athome/internal/prompt"
	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/run/runtest"
	"github.com/morrieinmaas/athome/internal/steps"
	"github.com/morrieinmaas/athome/internal/tui"
)

func TestSeedPrecedenceAndLocked(t *testing.T) {
	home, _ := tuiSandbox(t)
	chez := filepath.Join(home, ".config", "chezmoi")
	os.MkdirAll(chez, 0o755)
	os.WriteFile(filepath.Join(chez, "chezmoi.toml"), []byte("[data]\n  personalName = \"Stored\"\n  workName = \"StoredWork\"\n  nordvpnCountry = \"DE\"\n  meshProvider = \"netbird\"\n  includeAgents = false\n"), 0o644)
	cfg := filepath.Join(t.TempDir(), "a.toml")
	os.WriteFile(cfg, []byte("personalName = \"FromFile\"\nworkEmail = \"file@x.io\"\n"), 0o644)
	t.Setenv("ATHOME_WORK_EMAIL", "env@x.io")

	// A --config run never opens the form, so build the seed directly.
	o, err := parseInit([]string{"--config", cfg, "--machine", "work"})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	env, code := newInitEnv(o, &stdout, &stderr)
	if env == nil {
		t.Fatalf("newInitEnv exit %d: %s", code, stderr.String())
	}
	got := buildSeed(context.Background(), env, o, environMap())
	v := got.Values
	for k, want := range map[string]string{
		"workEmail":      "env@x.io",   // env beats file
		"personalName":   "FromFile",   // file beats stored
		"workName":       "StoredWork", // stored beats default
		"nordvpnCountry": "DE",
		"machineType":    "work",
		"hostname":       "testbox",
	} {
		if v[k] != want {
			t.Errorf("Values[%s] = %q, want %q", k, v[k], want)
		}
	}
	if !got.Locked["workEmail"] || !got.Locked["machineType"] {
		t.Errorf("env and flag fields must be locked: %v", got.Locked)
	}
	if got.Locked["personalName"] {
		t.Errorf("a config-file field is prefilled, not locked")
	}
	if got.KeyExists {
		t.Errorf("no ssh key exists")
	}
}

func TestSeedKeyExistsAndNoAgentsFlag(t *testing.T) {
	home, _ := tuiSandbox(t)
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	os.WriteFile(filepath.Join(home, ".ssh", "testbox_ed25519"), []byte("k"), 0o600)
	os.MkdirAll(filepath.Join(home, ".config", "chezmoi"), 0o755)
	os.WriteFile(filepath.Join(home, ".config", "chezmoi", "chezmoi.toml"), []byte("[data]\n  meshProvider = \"netbird\"\n"), 0o644)
	var got tui.Seed
	runForm = func(_ context.Context, _ io.Reader, _ io.Writer, s tui.Seed) (map[string]string, error) {
		got = s
		return nil, tui.ErrQuit
	}
	run2("init", "--no-agents")
	if !got.KeyExists {
		t.Error("KeyExists must be true when the key file exists")
	}
	if got.Values["meshProvider"] != "netbird" {
		t.Errorf("stored meshProvider must survive config defaults, got %q", got.Values["meshProvider"])
	}
	if got.Values["includeAgents"] != "false" || !got.Locked["includeAgents"] {
		t.Errorf("--no-agents must lock includeAgents=false: %v %v", got.Values["includeAgents"], got.Locked)
	}
}

// KeyExists follows the steps' key path, which uses chezmoi's hostname.
func TestSeedKeyExistsUsesTheStepsKeyPath(t *testing.T) {
	home, _ := tuiSandbox(t)
	f := runtest.NewFake()
	f.On("chezmoi", "execute-template", "{{ .chezmoi.hostname }}").Return(run.Result{Stdout: "chez\n"})
	newRunner = func() run.Runner { return f }
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	os.WriteFile(filepath.Join(home, ".ssh", "chez_ed25519"), []byte("k"), 0o600)
	var got tui.Seed
	runForm = func(_ context.Context, _ io.Reader, _ io.Writer, s tui.Seed) (map[string]string, error) {
		got = s
		return nil, tui.ErrQuit
	}
	run2("init")
	if !got.KeyExists {
		t.Error("KeyExists must look at the chezmoi hostname's key")
	}
}

func TestStoredChezmoiDataStringifiesBools(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".config", "chezmoi"), 0o755)
	os.WriteFile(filepath.Join(home, ".config", "chezmoi", "chezmoi.toml"), []byte("[data]\n  a = \"x\"\n  b = true\n  c = 3\n"), 0o644)
	got := storedChezmoiData(home)
	if got["a"] != "x" || got["b"] != "true" {
		t.Errorf("got %v", got)
	}
	if _, ok := got["c"]; ok {
		t.Error("non-string, non-bool values are skipped")
	}
}

// An auto-discovered answers file that does not set machine, includeAgents or
// meshProvider must not override what chezmoi.toml stores.
func TestSeedFileSettingNothingKeepsStoredChoices(t *testing.T) {
	home, _ := tuiSandbox(t)
	os.MkdirAll(filepath.Join(home, ".config", "chezmoi"), 0o755)
	os.WriteFile(filepath.Join(home, ".config", "chezmoi", "chezmoi.toml"),
		[]byte("[data]\n  machineType = \"work\"\n  includeAgents = false\n  meshProvider = \"netbird\"\n"), 0o644)
	os.MkdirAll(filepath.Join(home, ".config", "athome"), 0o755)
	os.WriteFile(filepath.Join(home, ".config", "athome", "bootstrap.toml"), []byte("personalName = \"FromFile\"\n"), 0o644)
	var got tui.Seed
	runForm = func(_ context.Context, _ io.Reader, _ io.Writer, s tui.Seed) (map[string]string, error) {
		got = s
		return nil, tui.ErrQuit
	}
	run2("init")
	for k, want := range map[string]string{
		"personalName": "FromFile", "machineType": "work", "includeAgents": "false", "meshProvider": "netbird",
	} {
		if got.Values[k] != want {
			t.Errorf("Values[%s] = %q, want %q", k, got.Values[k], want)
		}
	}
}

// When the file does set them, the file wins over the stored values.
func TestSeedFileSettingsBeatStored(t *testing.T) {
	home, _ := tuiSandbox(t)
	os.MkdirAll(filepath.Join(home, ".config", "chezmoi"), 0o755)
	os.WriteFile(filepath.Join(home, ".config", "chezmoi", "chezmoi.toml"),
		[]byte("[data]\n  machineType = \"work\"\n  includeAgents = false\n  meshProvider = \"netbird\"\n"), 0o644)
	os.MkdirAll(filepath.Join(home, ".config", "athome"), 0o755)
	os.WriteFile(filepath.Join(home, ".config", "athome", "bootstrap.toml"),
		[]byte("machine = \"personal\"\nincludeAgents = true\nmeshProvider = \"none\"\n"), 0o644)
	var got tui.Seed
	runForm = func(_ context.Context, _ io.Reader, _ io.Writer, s tui.Seed) (map[string]string, error) {
		got = s
		return nil, tui.ErrQuit
	}
	run2("init")
	for k, want := range map[string]string{"machineType": "personal", "includeAgents": "true", "meshProvider": "none"} {
		if got.Values[k] != want {
			t.Errorf("Values[%s] = %q, want %q", k, got.Values[k], want)
		}
	}
}

// A hostname answered in the form is written to chezmoi.toml for this run, but
// the ssh key keeps chezmoi's hostname: the form's key question is about that key.
func TestFormHostnameAnswerLeavesTheKeyOnChezmoisHostname(t *testing.T) {
	home, _ := tuiSandbox(t)
	f := runtest.NewFake()
	f.On("chezmoi", "execute-template", "{{ .chezmoi.hostname }}").Return(run.Result{Stdout: "chez\n"})
	newRunner = func() run.Runner { return f }
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	os.WriteFile(filepath.Join(home, ".ssh", "chez_ed25519"), []byte("k"), 0o600)
	runForm = func(_ context.Context, _ io.Reader, _ io.Writer, s tui.Seed) (map[string]string, error) {
		if !s.KeyExists {
			t.Error("chezmoi's hostname has a key")
		}
		return map[string]string{"machineType": "personal", "hostname": "box", "sshKeyAction": "use"}, nil
	}
	var action, keyPath string
	var asked bool
	runProgress = func(ctx context.Context, _ io.Reader, _ io.Writer, env *steps.Env, _ []steps.Step, _ func(func(func()))) (steps.Report, error) {
		env.Ask = prompt.New(failReader{&asked}, io.Discard)
		action = env.Cfg.Data["sshKeyAction"]
		keyPath = steps.KeyPath(ctx, env)
		return steps.Execute(ctx, env, steps.SSHKey())
	}
	if code, _, e := run2("init"); code != 0 {
		t.Fatalf("exit %d: %s", code, e)
	}
	if action != "use" || asked {
		t.Errorf("sshKeyAction = %q, asked = %v: the steps must not ask again", action, asked)
	}
	if want := filepath.Join(home, ".ssh", "chez_ed25519"); keyPath != want {
		t.Errorf("key path = %q, want %q", keyPath, want)
	}
}

// An import that creates the key makes the form's generate answer moot: the
// ssh step must use the imported key and not ask use/generate/reupload.
func TestImportedKeyIsUsedWithoutAsking(t *testing.T) {
	home, _ := tuiSandbox(t)
	from := t.TempDir()
	os.MkdirAll(filepath.Join(from, "ssh"), 0o700)
	os.WriteFile(filepath.Join(from, "ssh", "old_ed25519"), []byte("k"), 0o600)
	os.WriteFile(filepath.Join(from, "ssh", "old_ed25519.pub"), []byte("p"), 0o644)
	runForm = func(context.Context, io.Reader, io.Writer, tui.Seed) (map[string]string, error) {
		return map[string]string{"machineType": "personal", "generateSshKey": "true"}, nil
	}
	var asked bool
	runProgress = func(ctx context.Context, _ io.Reader, _ io.Writer, env *steps.Env, _ []steps.Step, _ func(func(func()))) (steps.Report, error) {
		env.Ask = prompt.New(failReader{&asked}, io.Discard)
		return steps.Execute(ctx, env, steps.SSHKey())
	}
	if code, _, e := run2("init", "--import-from", from, "--import-ssh"); code != 0 {
		t.Fatalf("exit %d: %s", code, e)
	}
	if asked {
		t.Error("the ssh step asked although the import created the key")
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "testbox_ed25519")); err != nil {
		t.Errorf("imported key missing: %v", err)
	}
}

func TestSeedDropsStoredGithubIdWhenHandleDiffers(t *testing.T) {
	stored := map[string]string{"githubHandle": "old", "githubId": "42"}
	dir := t.TempDir()
	write := func(body string) string {
		p := filepath.Join(dir, "a.toml")
		os.WriteFile(p, []byte(body), 0o644)
		return p
	}

	// handle from env
	cfg := config.Config{Data: map[string]string{"githubHandle": "new"}}
	vals, _ := seedPrecedence(stored, cfg, initOpts{}, map[string]string{"ATHOME_GITHUB_HANDLE": "new"})
	if vals["githubId"] != "" {
		t.Errorf("env handle: stale id kept: %q", vals["githubId"])
	}

	// handle from file
	cfg = config.Config{Data: map[string]string{"githubHandle": "new"}, Source: write("githubHandle = \"new\"\n")}
	vals, _ = seedPrecedence(stored, cfg, initOpts{}, map[string]string{})
	if vals["githubId"] != "" {
		t.Errorf("file handle: stale id kept: %q", vals["githubId"])
	}

	// id set by the file alongside the handle is kept
	cfg = config.Config{Data: map[string]string{"githubHandle": "new", "githubId": "7"}, Source: write("githubHandle = \"new\"\ngithubId = \"7\"\n")}
	vals, _ = seedPrecedence(stored, cfg, initOpts{}, map[string]string{})
	if vals["githubId"] != "7" {
		t.Errorf("file id = %q, want 7", vals["githubId"])
	}

	// same handle keeps the stored id
	cfg = config.Config{Data: map[string]string{"githubHandle": "old"}}
	vals, _ = seedPrecedence(stored, cfg, initOpts{}, map[string]string{"ATHOME_GITHUB_HANDLE": "old"})
	if vals["githubId"] != "42" {
		t.Errorf("same handle: id = %q, want 42", vals["githubId"])
	}
}

// ctxRunner records whether the context handed to Run carries a deadline.
type ctxRunner struct{ hasDeadline bool }

func (c *ctxRunner) Run(ctx context.Context, _ run.Cmd) (run.Result, error) {
	_, c.hasDeadline = ctx.Deadline()
	return run.Result{Stdout: "octo\n"}, nil
}
func (c *ctxRunner) LookPath(string) (string, error) { return "/usr/bin/gh", nil }

// `gh api user` runs before the form: a hung gh must not block it for ever.
func TestSeedDerivedDefaultsBoundsGhLookup(t *testing.T) {
	r := &ctxRunner{}
	vals := map[string]string{"hostname": "h"}
	seedDerivedDefaults(context.Background(), &steps.Env{Run: r}, vals)
	if !r.hasDeadline {
		t.Error("gh api user ran without a timeout")
	}
	if vals["githubHandle"] != "octo" {
		t.Errorf("handle = %q", vals["githubHandle"])
	}
}
