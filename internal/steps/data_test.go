package steps_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/morrieinmaas/athome/internal/config"
	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
)

// testdata/chezmoi-data.golden.toml is the ~/.config/chezmoi/chezmoi.toml that
// scripts/bootstrap-legacy.sh writes for test/e2e/bootstrap.toml (lines 875-900). It was
// captured, without running the bootstrap, from the repo root with:
//
//	H=$(mktemp -d) && HOME=$H HOSTNAME=athome-test bash -c '
//	c_blue(){ :; }
//	short_hostname() { printf "%s\n" "${HOSTNAME%%.*}"; }
//	MACHINE=personal; INCLUDE_AGENTS=true; MACHINE_FROM_FLAG=false; AGENTS_FROM_FLAG=false
//	eval "$(/usr/bin/sed -n "182,216p" scripts/bootstrap-legacy.sh)"
//	load_config test/e2e/bootstrap.toml
//	gh_handle=$ATHOME_GITHUB_HANDLE; gh_id=$ATHOME_GITHUB_ID
//	eval "$(/usr/bin/sed -n "875,900p" scripts/bootstrap-legacy.sh)"
//	cat "$chezmoi_config"' > internal/steps/testdata/chezmoi-data.golden.toml
//
// Lines 182-216 are cfg_get and load_config, 875-900 the [data] block. The only
// machine-specific value is hostname (HOSTNAME=athome-test here), so the golden
// comparison checks that key separately.

func chezmoiTomlPath(fx *fixture) string { return fx.Path(".config", "chezmoi", "chezmoi.toml") }

func readChezmoiData(t *testing.T, path string) map[string]any {
	t.Helper()
	var doc struct {
		Data map[string]any `toml:"data"`
	}
	if _, err := toml.DecodeFile(path, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return doc.Data
}

func e2eConfig(t *testing.T, home string) config.Config {
	t.Helper()
	cfg, err := config.Load(config.Options{
		ConfigPath: filepath.Join("..", "..", "test", "e2e", "bootstrap.toml"),
		Home:       home,
		Env:        map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestDataGolden(t *testing.T) {
	fx := newFixture(t)
	fx.Env.Cfg = e2eConfig(t, fx.Home)
	fx.MustExec(steps.Data())

	got := readChezmoiData(t, chezmoiTomlPath(fx))
	want := readChezmoiData(t, filepath.Join("testdata", "chezmoi-data.golden.toml"))

	for k, w := range want {
		g, ok := got[k]
		if !ok {
			t.Errorf("[data] lacks key %q", k)
			continue
		}
		if k == "hostname" {
			if s, _ := g.(string); s == "" || strings.Contains(s, ".") {
				t.Errorf("hostname = %#v, want a non-empty short hostname", g)
			}
			continue
		}
		if g != w {
			t.Errorf("[data] %s = %#v, golden has %#v", k, g, w)
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("[data] has extra key %q not in the golden", k)
		}
	}
}

func TestDataMachineAndAgents(t *testing.T) {
	fx := newFixture(t)
	fx.Env.Cfg = e2eConfig(t, fx.Home)
	fx.Env.Cfg.Machine = "work"
	fx.Env.Cfg.IncludeAgents = true
	fx.MustExec(steps.Data())
	got := readChezmoiData(t, chezmoiTomlPath(fx))
	if got["machineType"] != "work" {
		t.Errorf("machineType = %#v, want work", got["machineType"])
	}
	if got["includeAgents"] != true {
		t.Errorf("includeAgents = %#v, want the TOML boolean true", got["includeAgents"])
	}
}

func TestDataEscaping(t *testing.T) {
	fx := newFixture(t)
	fx.Env.Cfg.Data = map[string]string{
		"githubHandle": "octo", "githubId": "1",
		"personalName": `Zoë "Z" back\slash # not=comment`,
		"workName":     "日本語 name",
	}
	fx.MustExec(steps.Data())
	got := readChezmoiData(t, chezmoiTomlPath(fx))
	for k, want := range map[string]string{"personalName": fx.Env.Cfg.Data["personalName"], "workName": "日本語 name"} {
		if got[k] != want {
			t.Errorf("%s round-trips to %#v, want %q", k, got[k], want)
		}
	}
}

func TestDataMeshDefault(t *testing.T) {
	fx := newFixture(t)
	fx.Env.Cfg.Data = map[string]string{"githubHandle": "octo", "githubId": "1"}
	fx.MustExec(steps.Data())
	if got := readChezmoiData(t, chezmoiTomlPath(fx)); got["meshProvider"] != "meshnet" {
		t.Errorf("meshProvider = %#v, want the default meshnet", got["meshProvider"])
	}
}

func TestIdentity(t *testing.T) {
	data := func(extra map[string]string) map[string]string {
		m := map[string]string{}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}

	t.Run("handle from the config, no gh call for it", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.Have("gh")
		fx.Env.Cfg.Data = data(map[string]string{"githubHandle": "octo", "githubId": "5"})
		fx.MustExec(steps.Data())
		if fx.called("gh", "api", "user") {
			t.Errorf("handle known, `gh api user` must not run:\n%s", fx.dump())
		}
		if fx.called("gh", "api", "users/octo") {
			t.Errorf("id known, `gh api users/octo` must not run:\n%s", fx.dump())
		}
	})
	t.Run("handle then id from gh", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.Have("gh")
		fx.Fake.On("gh", "api", "user", "--jq", ".login").Return(run.Result{Stdout: "octo\n"})
		fx.Fake.On("gh", "api", "users/octo", "--jq", ".id").Return(run.Result{Stdout: "42\n"})
		fx.Env.Cfg.Data = data(nil)
		fx.MustExec(steps.Data())
		got := readChezmoiData(t, chezmoiTomlPath(fx))
		if got["githubHandle"] != "octo" || got["githubId"] != "42" {
			t.Errorf("handle=%#v id=%#v, want octo and 42", got["githubHandle"], got["githubId"])
		}
	})
	t.Run("id from gh when the handle is configured", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.Have("gh")
		fx.Fake.On("gh", "api", "users/octo", "--jq", ".id").Return(run.Result{Stdout: "42\n"})
		fx.Env.Cfg.Data = data(map[string]string{"githubHandle": "octo"})
		fx.MustExec(steps.Data())
		if got := readChezmoiData(t, chezmoiTomlPath(fx)); got["githubId"] != "42" {
			t.Errorf("githubId = %#v, want 42", got["githubId"])
		}
		if fx.called("gh", "api", "user") {
			t.Errorf("handle configured, must not ask gh who is logged in")
		}
	})
	t.Run("id from the public API via curl when gh is absent", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.Have("curl")
		fx.Fake.On("curl", "-fsSL", "https://api.github.com/users/octo").Return(run.Result{Stdout: "{\n  \"login\": \"octo\",\n  \"id\": 99,\n  \"node_id\": \"x\"\n}\n"})
		fx.Env.Cfg.Data = data(map[string]string{"githubHandle": "octo"})
		fx.MustExec(steps.Data())
		if got := readChezmoiData(t, chezmoiTomlPath(fx)); got["githubId"] != "99" {
			t.Errorf("githubId = %#v, want 99", got["githubId"])
		}
	})
	t.Run("no handle and no gh: empty values, no error", func(t *testing.T) {
		fx := newFixture(t)
		fx.Env.Cfg.Data = data(nil)
		fx.MustExec(steps.Data())
		got := readChezmoiData(t, chezmoiTomlPath(fx))
		if got["githubHandle"] != "" || got["githubId"] != "" {
			t.Errorf("handle=%#v id=%#v, want both empty", got["githubHandle"], got["githubId"])
		}
	})
}

func TestDataIdempotent(t *testing.T) {
	fx := newFixture(t)
	fx.Env.Cfg = e2eConfig(t, fx.Home)
	fx.MustExec(steps.Data())
	before := readFile(t, chezmoiTomlPath(fx))

	rep := fx.MustRerun(steps.Data())
	if len(rep.Applied) != 0 {
		t.Errorf("second run applied %v, want nothing", rep.Applied)
	}
	if after := readFile(t, chezmoiTomlPath(fx)); after != before {
		t.Errorf("second run changed the file")
	}
}

// An offline rerun derives no id; the id stored by the online run must stay,
// and the step must not rewrite the file.
func TestDataKeepsStoredIdentityWhenOffline(t *testing.T) {
	for _, handleInCfg := range []bool{true, false} {
		name := "handle derived empty"
		if handleInCfg {
			name = "handle from the config"
		}
		t.Run(name, func(t *testing.T) {
			fx := newFixture(t)
			fx.Fake.Have("gh")
			fx.Fake.On("gh", "api", "user", "--jq", ".login").Return(run.Result{Stdout: "octo\n"})
			fx.Fake.On("gh", "api", "users/octo", "--jq", ".id").Return(run.Result{Stdout: "42\n"})
			if handleInCfg {
				fx.Env.Cfg.Data = map[string]string{"githubHandle": "octo"}
			}
			fx.MustExec(steps.Data())
			before := readFile(t, chezmoiTomlPath(fx))

			fx.Fake.On("gh", "api", "user", "--jq", ".login").Return(run.Result{Code: 1})
			fx.Fake.On("gh", "api", "users/octo", "--jq", ".id").Return(run.Result{Code: 1})
			rep := fx.MustRerun(steps.Data())
			if len(rep.Applied) != 0 {
				t.Errorf("offline rerun applied %v, want nothing", rep.Applied)
			}
			got := readChezmoiData(t, chezmoiTomlPath(fx))
			if got["githubHandle"] != "octo" || got["githubId"] != "42" {
				t.Errorf("handle=%#v id=%#v, want the stored octo and 42", got["githubHandle"], got["githubId"])
			}
			if after := readFile(t, chezmoiTomlPath(fx)); after != before {
				t.Errorf("offline rerun changed the file")
			}
		})
	}
}

// A stored id belongs to the stored handle; for another handle it is wrong, so
// an offline run with a changed handle leaves the id empty.
func TestDataDropsStoredIDWhenHandleChanges(t *testing.T) {
	fx := newFixture(t)
	fx.Fake.Have("gh")
	fx.Fake.On("gh", "api", "users/octo", "--jq", ".id").Return(run.Result{Stdout: "42\n"})
	fx.Env.Cfg.Data = map[string]string{"githubHandle": "octo"}
	fx.MustExec(steps.Data())

	fx.Fake.On("gh", "api", "users/other", "--jq", ".id").Return(run.Result{Code: 1})
	fx.Env.Cfg.Data = map[string]string{"githubHandle": "other"}
	fx.MustRerun(steps.Data())
	got := readChezmoiData(t, chezmoiTomlPath(fx))
	if got["githubHandle"] != "other" || got["githubId"] != "" {
		t.Errorf("handle=%#v id=%#v, want other and an empty id", got["githubHandle"], got["githubId"])
	}
}

func TestDataRewrittenWhenAnswersChange(t *testing.T) {
	fx := newFixture(t)
	fx.Env.Cfg = e2eConfig(t, fx.Home)
	fx.MustExec(steps.Data())

	fx.Env.Cfg.Data["personalName"] = "someone else"
	rep := fx.MustRerun(steps.Data())
	if len(rep.Applied) == 0 {
		t.Errorf("a changed answer must be written")
	}
	if got := readChezmoiData(t, chezmoiTomlPath(fx)); got["personalName"] != "someone else" {
		t.Errorf("personalName = %#v, want someone else", got["personalName"])
	}
}

func TestDataKeepsSourceDirPin(t *testing.T) {
	fx := newFixture(t)
	fx.Env.Cfg = e2eConfig(t, fx.Home)
	fx.MustExec(steps.Data())
	path := chezmoiTomlPath(fx)
	pinned := "sourceDir = \"" + filepath.Join(fx.Repo, "home") + "\"\n" + readFile(t, path)
	if err := os.WriteFile(path, []byte(pinned), 0o600); err != nil {
		t.Fatal(err)
	}
	rep := fx.MustRerun(steps.Data())
	if len(rep.Applied) != 0 {
		t.Errorf("a sourceDir pin ahead of an unchanged [data] must not trigger a rewrite, applied %v", rep.Applied)
	}
}

// F2: chezmoi init re-renders chezmoi.toml with extra keys and tables around the
// seed; the step is done once every seed key has its wanted value.
func TestDataIgnoresExtraKeys(t *testing.T) {
	fx := newFixture(t)
	fx.Env.Cfg = e2eConfig(t, fx.Home)
	fx.MustExec(steps.Data())
	path := chezmoiTomlPath(fx)
	rendered := "[git]\n    command = \"git\"\n" + readFile(t, path) + "    source_dir_extra = \"x\"\n"
	if err := os.WriteFile(path, []byte(rendered), 0o600); err != nil {
		t.Fatal(err)
	}
	if rep := fx.MustRerun(steps.Data()); len(rep.Applied) != 0 {
		t.Errorf("extra keys must not trigger a rewrite, applied %v", rep.Applied)
	}
	if got := readFile(t, path); got != rendered {
		t.Errorf("the rendered config was overwritten:\n%s", got)
	}
}

// F4: [data].hostname is bash's short hostname, not chezmoi's .chezmoi.hostname.
func TestDataHostnameIsTheShortHostname(t *testing.T) {
	fx := newFixture(t)
	t.Setenv("HOSTNAME", "short.example.org")
	fx.MustExec(steps.Data()) // chezmoi reports testhost
	if got := readChezmoiData(t, chezmoiTomlPath(fx))["hostname"]; got != "short" {
		t.Errorf("hostname = %#v, want short", got)
	}
}

// F5: $HOSTNAME, then os.Hostname(), then /etc/hostname.
func TestShortHostnameOrder(t *testing.T) {
	etc := filepath.Join(t.TempDir(), "hostname")
	writeFile(t, etc, "fromfile.lan\n", 0o644)
	host := func(h string, err error) func() (string, error) {
		return func() (string, error) { return h, err }
	}
	cases := []struct {
		name, env string
		osHost    func() (string, error)
		want      string
	}{
		{"env wins", "fromenv.x", host("fromos", nil), "fromenv"},
		{"os.Hostname before /etc/hostname", "", host("fromos.x", nil), "fromos"},
		{"/etc/hostname last", "", host("", errors.New("no")), "fromfile"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("HOSTNAME", c.env)
			t.Cleanup(steps.SetHostnameSources(c.osHost, etc))
			fx := newFixture(t)
			fx.MustExec(steps.Data())
			if got := readChezmoiData(t, chezmoiTomlPath(fx))["hostname"]; got != c.want {
				t.Errorf("hostname = %#v, want %q", got, c.want)
			}
		})
	}
}

// C5: one run looks the identity up once, however many times Check and Apply
// need it; a later run looks it up again.
func TestDataIdentityLookedUpOncePerRun(t *testing.T) {
	fx := newFixture(t)
	fx.Fake.Have("gh")
	fx.Fake.On("gh", "api", "user", "--jq", ".login").Return(run.Result{Stdout: "octo\n"})
	fx.Fake.On("gh", "api", "users/octo", "--jq", ".id").Return(run.Result{Stdout: "7\n"})
	list := steps.Data()
	fx.MustExec(list)
	if n := len(fx.callsTo("gh", "api", "user")); n != 1 {
		t.Errorf("gh api user ran %d times in one run, want 1:\n%s", n, fx.dump())
	}
	if n := len(fx.callsTo("gh", "api", "users/octo")); n != 1 {
		t.Errorf("gh api users/octo ran %d times in one run, want 1:\n%s", n, fx.dump())
	}
	if got := readChezmoiData(t, chezmoiTomlPath(fx)); got["githubHandle"] != "octo" || got["githubId"] != "7" {
		t.Errorf("identity = %v/%v, want octo/7", got["githubHandle"], got["githubId"])
	}
	fx.MustRerun(list)
	if n := len(fx.callsTo("gh", "api", "user")); n != 2 {
		t.Errorf("the second run must look the identity up again, gh api user ran %d times in total, want 2", n)
	}
}

func TestDataUnicodeNamesRoundTrip(t *testing.T) {
	fx := newFixture(t)
	fx.Env.Cfg.Data = map[string]string{
		"githubHandle": "octo", "githubId": "1",
		"personalName": "Jürgen Müller", "workName": "山田", "sidebizName": "Zoë Ålesund",
	}
	fx.MustExec(steps.Data())
	got := readChezmoiData(t, chezmoiTomlPath(fx))
	for k, want := range map[string]string{"personalName": "Jürgen Müller", "workName": "山田", "sidebizName": "Zoë Ålesund"} {
		if got[k] != want {
			t.Errorf("%s = %#v, want %q", k, got[k], want)
		}
	}
}

// A bitwardenUrl the init form answered, even empty (US cloud), is not asked again.
func TestDataSkipsBitwardenPromptWhenFormAnswered(t *testing.T) {
	fx := newFixture(t)
	fx.Env.Cfg.Data = map[string]string{"githubHandle": "octo", "githubId": "1", "useBitwarden": "true", "bitwardenUrl": ""}
	fx.Env.Answered = map[string]bool{"bitwardenUrl": true}
	presetAsk(t, fx) // fails the test on any read
	fx.MustExec(steps.Data())
	if got := readChezmoiData(t, chezmoiTomlPath(fx))["bitwardenUrl"]; got != "" {
		t.Errorf("bitwardenUrl = %#v, want empty", got)
	}
}

// An explicit hostname answer reaches chezmoi.toml and the key path without touching $HOSTNAME.
func TestDataHonoursAnsweredHostname(t *testing.T) {
	fx := newFixture(t)
	t.Setenv("HOSTNAME", "envhost")
	fx.Env.Cfg.Data = map[string]string{"githubHandle": "octo", "githubId": "1", "hostname": "answered"}
	fx.MustExec(steps.Data())
	if got := readChezmoiData(t, chezmoiTomlPath(fx))["hostname"]; got != "answered" {
		t.Errorf("hostname = %#v, want answered", got)
	}
	if got, want := steps.KeyPath(context.Background(), fx.Env), fx.Path(".ssh", "answered_ed25519"); got != want {
		t.Errorf("KeyPath = %q, want %q", got, want)
	}
}

func TestStoredDataReadsTheChezmoiToml(t *testing.T) {
	home := t.TempDir()
	if got := steps.StoredData(home); got != nil {
		t.Errorf("missing file: %v", got)
	}
	writeFile(t, filepath.Join(home, ".config", "chezmoi", "chezmoi.toml"), "[data]\n  a = \"x\"\n  b = true\n", 0o644)
	got := steps.StoredData(home)
	if got["a"] != "x" || got["b"] != true {
		t.Errorf("StoredData = %v", got)
	}
}
