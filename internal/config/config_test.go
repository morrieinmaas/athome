package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/config"
)

// keyTable is the answer table of scripts/bootstrap.sh (B:189-209): the key in
// the toml file, its ATHOME_* env var and its default when neither is set.
var keyTable = []struct {
	key, env, def string
}{
	{"githubHandle", "ATHOME_GITHUB_HANDLE", ""},
	{"githubId", "ATHOME_GITHUB_ID", ""},
	{"personalName", "ATHOME_PERSONAL_NAME", ""},
	{"workEmail", "ATHOME_WORK_EMAIL", ""},
	{"workName", "ATHOME_WORK_NAME", ""},
	{"sidebizEmail", "ATHOME_SIDEBIZ_EMAIL", ""},
	{"sidebizName", "ATHOME_SIDEBIZ_NAME", ""},
	{"agentsRepo", "ATHOME_AGENTS_REPO", ""},
	{"slackDefaultWorkspace", "ATHOME_SLACK_WORKSPACE", ""},
	{"slackTeamId", "ATHOME_SLACK_TEAM_ID", ""},
	{"matterhornHost", "ATHOME_MATTERHORN_HOST", ""},
	{"matterhornUser", "ATHOME_MATTERHORN_USER", ""},
	{"netbirdManagementUrl", "ATHOME_NETBIRD_MGMT_URL", ""},
	{"nordvpnCountry", "ATHOME_NORDVPN_COUNTRY", ""},
	{"meshProvider", "ATHOME_MESH_PROVIDER", "meshnet"},
	{"extraPackages", "ATHOME_EXTRA_PACKAGES", ""},
	{"useBitwarden", "ATHOME_USE_BITWARDEN", ""},
	{"sshKeyAction", "ATHOME_SSH_KEY_ACTION", ""},
	{"generateSshKey", "ATHOME_GENERATE_SSH_KEY", ""},
	{"bitwardenUrl", "ATHOME_BW_BASE_URL", ""},
}

func writeFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPrecedence(t *testing.T) {
	for _, k := range keyTable {
		t.Run(k.key, func(t *testing.T) {
			dir := t.TempDir()
			file := writeFile(t, filepath.Join(dir, "a.toml"), k.key+` = "from-file"`+"\n")

			got, err := config.Load(config.Options{ConfigPath: file, Home: dir, Env: map[string]string{k.env: "from-env"}})
			if err != nil {
				t.Fatal(err)
			}
			if got.Data[k.key] != "from-env" {
				t.Errorf("env over file: Data[%s] = %q, want from-env", k.key, got.Data[k.key])
			}

			got, err = config.Load(config.Options{ConfigPath: file, Home: dir, Env: map[string]string{}})
			if err != nil {
				t.Fatal(err)
			}
			if got.Data[k.key] != "from-file" {
				t.Errorf("file over default: Data[%s] = %q, want from-file", k.key, got.Data[k.key])
			}

			got, err = config.Load(config.Options{Home: dir, RepoRoot: dir, Env: map[string]string{}})
			if err != nil {
				t.Fatal(err)
			}
			if got.Data[k.key] != k.def {
				t.Errorf("default: Data[%s] = %q, want %q", k.key, got.Data[k.key], k.def)
			}

			got, err = config.Load(config.Options{Home: dir, RepoRoot: dir, Env: map[string]string{k.env: "only-env"}})
			if err != nil {
				t.Fatal(err)
			}
			if got.Data[k.key] != "only-env" {
				t.Errorf("env over default: Data[%s] = %q, want only-env", k.key, got.Data[k.key])
			}
		})
	}
}

// bash fills a blank with `: "${VAR:=file value}"`, which also replaces an
// env var that is set but empty.
func TestEmptyEnvFallsThroughToFile(t *testing.T) {
	dir := t.TempDir()
	file := writeFile(t, filepath.Join(dir, "a.toml"), `githubHandle = "from-file"`+"\n")
	got, err := config.Load(config.Options{ConfigPath: file, Home: dir, Env: map[string]string{"ATHOME_GITHUB_HANDLE": ""}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Data["githubHandle"] != "from-file" {
		t.Errorf("githubHandle = %q, want from-file", got.Data["githubHandle"])
	}
}

func TestMachineAndAgentsPrecedence(t *testing.T) {
	dir := t.TempDir()
	file := writeFile(t, filepath.Join(dir, "a.toml"), "machine = \"work\"\nincludeAgents = \"false\"\n")

	got, err := config.Load(config.Options{ConfigPath: file, Home: dir})
	if err != nil {
		t.Fatal(err)
	}
	if got.Machine != "work" || got.IncludeAgents {
		t.Errorf("file only: machine=%q includeAgents=%v, want work false", got.Machine, got.IncludeAgents)
	}

	got, err = config.Load(config.Options{ConfigPath: file, Home: dir, Machine: "personal", NoAgents: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Machine != "personal" {
		t.Errorf("--machine must beat the file: got %q", got.Machine)
	}
	if got.IncludeAgents {
		t.Errorf("--no-agents must beat the file")
	}

	file = writeFile(t, filepath.Join(dir, "b.toml"), "includeAgents = \"true\"\n")
	got, err = config.Load(config.Options{ConfigPath: file, Home: dir, NoAgents: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.IncludeAgents {
		t.Errorf("--no-agents must beat includeAgents = true in the file")
	}
	got, err = config.Load(config.Options{ConfigPath: file, Home: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !got.IncludeAgents {
		t.Errorf("includeAgents = true in the file must give true")
	}
}

func TestDiscovery(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	flagFile := writeFile(t, filepath.Join(t.TempDir(), "flag.toml"), `githubHandle = "from-flag"`+"\n")
	repoFile := writeFile(t, filepath.Join(repo, "bootstrap.local.toml"), `githubHandle = "from-repo"`+"\n")
	homeFile := writeFile(t, filepath.Join(home, ".config", "athome", "bootstrap.toml"), `githubHandle = "from-home"`+"\n")

	cases := []struct {
		name       string
		opts       config.Options
		wantHandle string
		wantSource string
	}{
		{"all three present picks --config", config.Options{ConfigPath: flagFile, RepoRoot: repo, Home: home}, "from-flag", flagFile},
		{"repo local beats home", config.Options{RepoRoot: repo, Home: home}, "from-repo", repoFile},
		{"home when it is the only one", config.Options{RepoRoot: t.TempDir(), Home: home}, "from-home", homeFile},
		{"none present", config.Options{RepoRoot: t.TempDir(), Home: t.TempDir()}, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.opts.Env = map[string]string{}
			got, err := config.Load(c.opts)
			if err != nil {
				t.Fatal(err)
			}
			if got.Data["githubHandle"] != c.wantHandle {
				t.Errorf("githubHandle = %q, want %q", got.Data["githubHandle"], c.wantHandle)
			}
			if got.Source != c.wantSource {
				t.Errorf("Source = %q, want %q", got.Source, c.wantSource)
			}
		})
	}
}

func TestQuotedHash(t *testing.T) {
	dir := t.TempDir()
	file := writeFile(t, filepath.Join(dir, "a.toml"),
		"personalName = \"x=y # z\"\nworkName = \"a # b\"\nworkEmail = \"k=v\"\n")
	got, err := config.Load(config.Options{ConfigPath: file, Home: dir, Env: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"personalName": "x=y # z", "workName": "a # b", "workEmail": "k=v"} {
		if got.Data[key] != want {
			t.Errorf("Data[%s] = %q, want %q", key, got.Data[key], want)
		}
	}
}

// Same minimal parser as bash: `key = value` or `key = "value"`, indentation
// and trailing blanks allowed, comment lines and unknown keys ignored.
func TestParserShapes(t *testing.T) {
	dir := t.TempDir()
	file := writeFile(t, filepath.Join(dir, "a.toml"),
		"# a comment line\n"+
			"  githubHandle   =   \"padded\"   \n"+
			"personalName = bare value\n"+
			"unknownKey = \"ignored\"\n"+
			"workName = \"Zoë Ünïcode\"\n"+
			"githubId = \"1\"\ngithubId = \"2\"\n")
	got, err := config.Load(config.Options{ConfigPath: file, Home: dir, Env: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"githubHandle": "padded",
		"personalName": "bare value",
		"workName":     "Zoë Ünïcode",
		"githubId":     "1", // first match wins, as in cfg_get
	} {
		if got.Data[key] != want {
			t.Errorf("Data[%s] = %q, want %q", key, got.Data[key], want)
		}
	}
}

func TestEmptyFile(t *testing.T) {
	dir := t.TempDir()
	file := writeFile(t, filepath.Join(dir, "empty.toml"), "")
	got, err := config.Load(config.Options{ConfigPath: file, Home: dir, Env: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Machine != "personal" {
		t.Errorf("Machine = %q, want personal", got.Machine)
	}
	if !got.IncludeAgents {
		t.Errorf("IncludeAgents = false, want true")
	}
	if got.Data["meshProvider"] != "meshnet" {
		t.Errorf("meshProvider = %q, want meshnet", got.Data["meshProvider"])
	}
	if got.Data["githubHandle"] != "" {
		t.Errorf("githubHandle = %q, want empty", got.Data["githubHandle"])
	}
}

func TestBadValues(t *testing.T) {
	dir := t.TempDir()
	badMachine := writeFile(t, filepath.Join(dir, "m.toml"), "machine = \"laptop\"\n")
	badAgents := writeFile(t, filepath.Join(dir, "a.toml"), "includeAgents = \"maybe\"\n")

	cases := []struct {
		name string
		opts config.Options
		want string
	}{
		{"machine flag", config.Options{Machine: "laptop", Home: dir}, "laptop"},
		{"empty machine flag", config.Options{MachineSet: true, Home: dir}, "machine must be personal|work|remote (got: )"},
		{"machine in file", config.Options{ConfigPath: badMachine, Home: dir}, "laptop"},
		{"includeAgents in file", config.Options{ConfigPath: badAgents, Home: dir}, "maybe"},
		{"unreadable config", config.Options{ConfigPath: filepath.Join(dir, "missing.toml"), Home: dir}, "missing.toml"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.opts.Env = map[string]string{}
			_, err := config.Load(c.opts)
			if err == nil {
				t.Fatal("want an error")
			}
			if !errors.Is(err, config.ErrUsage) {
				t.Errorf("error %v does not wrap ErrUsage", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not name %q", err, c.want)
			}
		})
	}
}

// A flag beats a bad file value, as bash never reads machine from the file
// when --machine was given.
func TestMachineFlagShadowsBadFileValue(t *testing.T) {
	dir := t.TempDir()
	file := writeFile(t, filepath.Join(dir, "m.toml"), "machine = \"laptop\"\n")
	got, err := config.Load(config.Options{ConfigPath: file, Home: dir, Machine: "work", Env: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Machine != "work" {
		t.Errorf("Machine = %q, want work", got.Machine)
	}
}

func TestE2EBootstrapToml(t *testing.T) {
	home := t.TempDir()
	got, err := config.Load(config.Options{ConfigPath: filepath.Join("..", "..", "test", "e2e", "bootstrap.toml"), Home: home, Env: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Machine != "personal" || got.IncludeAgents {
		t.Errorf("machine=%q includeAgents=%v, want personal false", got.Machine, got.IncludeAgents)
	}
	want := map[string]string{
		"githubHandle": "athome-ci", "githubId": "0", "personalName": "athome ci",
		"workEmail": "", "workName": "", "sidebizEmail": "", "sidebizName": "",
		"agentsRepo": "", "netbirdManagementUrl": "", "nordvpnCountry": "", "bitwardenUrl": "",
		"meshProvider": "meshnet",
	}
	for k, v := range want {
		if got.Data[k] != v {
			t.Errorf("Data[%s] = %q, want %q", k, got.Data[k], v)
		}
	}
}

func TestSpacesInPaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "my home", "répo")
	file := writeFile(t, filepath.Join(root, "bootstrap.local.toml"), `githubHandle = "spacey"`+"\n")
	got, err := config.Load(config.Options{RepoRoot: root, Home: filepath.Join(root, "h o m e"), Env: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Data["githubHandle"] != "spacey" || got.Source != file {
		t.Errorf("handle=%q source=%q, want spacey %q", got.Data["githubHandle"], got.Source, file)
	}
}

// S7: chezmoi's TOML needs UTF-8, so a bad byte is caught here, naming the key.
func TestNonUTF8Answer(t *testing.T) {
	dir := t.TempDir()
	file := writeFile(t, filepath.Join(dir, "b.toml"), "workName = \"Ren\xe9\"\n")
	cases := []struct {
		name string
		opts config.Options
		want string
	}{
		{"env", config.Options{Home: dir, Env: map[string]string{"ATHOME_PERSONAL_NAME": "Ren\xe9"}}, "personalName"},
		{"file", config.Options{ConfigPath: file, Home: dir, Env: map[string]string{}}, "workName"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := config.Load(c.opts)
			if !errors.Is(err, config.ErrUsage) {
				t.Fatalf("err = %v, want ErrUsage", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not name %q", err, c.want)
			}
		})
	}
}

func TestRemoteMachine(t *testing.T) {
	load := func(t *testing.T, file string, env map[string]string) (config.Config, error) {
		t.Helper()
		dir := t.TempDir()
		p := writeFile(t, filepath.Join(dir, "a.toml"), file)
		return config.Load(config.Options{ConfigPath: p, Home: dir, Env: env})
	}
	ok := "machine = \"remote\"\nscope = \"work/acme\"\nmachineName = \"acme-dev\"\n"

	t.Run("accepted with scope and name", func(t *testing.T) {
		c, err := load(t, ok, nil)
		if err != nil || c.Machine != "remote" || c.Data["scope"] != "work/acme" || c.Data["machineName"] != "acme-dev" {
			t.Fatalf("got %+v, %v", c, err)
		}
	})
	t.Run("env over file", func(t *testing.T) {
		c, err := load(t, ok, map[string]string{"ATHOME_SCOPE": "sidebiz/x/y", "ATHOME_TIMEZONE": "Europe/Amsterdam", "ATHOME_LOCALE": "en_US.UTF-8"})
		if err != nil || c.Data["scope"] != "sidebiz/x/y" || c.Data["timezone"] != "Europe/Amsterdam" || c.Data["locale"] != "en_US.UTF-8" {
			t.Fatalf("got %+v, %v", c.Data, err)
		}
	})
	for name, file := range map[string]string{
		"no scope":       "machine = \"remote\"\nmachineName = \"a\"\n",
		"bare context":   "machine = \"remote\"\nscope = \"work\"\nmachineName = \"a\"\n",
		"dotdot":         "machine = \"remote\"\nscope = \"work/../x\"\nmachineName = \"a\"\n",
		"dot segment":    "machine = \"remote\"\nscope = \"work/./x\"\nmachineName = \"a\"\n",
		"bad context":    "machine = \"remote\"\nscope = \"home/x\"\nmachineName = \"a\"\n",
		"space":          "machine = \"remote\"\nscope = \"work/a b\"\nmachineName = \"a\"\n",
		"no machineName": "machine = \"remote\"\nscope = \"work/a\"\n",
		"upper name":     "machine = \"remote\"\nscope = \"work/a\"\nmachineName = \"Box\"\n",
		"leading dash":   "machine = \"remote\"\nscope = \"work/a\"\nmachineName = \"-x\"\n",
		"dotted name":    "machine = \"remote\"\nscope = \"work/a\"\nmachineName = \"a.b\"\n",
		"local name":     "machine = \"remote\"\nscope = \"work/a\"\nmachineName = \"local\"\n",
		"zero name":      "machine = \"remote\"\nscope = \"work/a\"\nmachineName = \"0\"\n",
		"trailing slash": "machine = \"remote\"\nscope = \"work/a/\"\nmachineName = \"a\"\n",
	} {
		t.Run("refused: "+name, func(t *testing.T) {
			if _, err := load(t, file, nil); !errors.Is(err, config.ErrUsage) {
				t.Fatalf("err = %v, want ErrUsage", err)
			}
		})
	}
	t.Run("personal and work need none", func(t *testing.T) {
		for _, m := range []string{"personal", "work"} {
			if _, err := load(t, "machine = \""+m+"\"\n", nil); err != nil {
				t.Errorf("%s: %v", m, err)
			}
		}
	})
	t.Run("message names remote", func(t *testing.T) {
		_, err := load(t, "machine = \"bogus\"\n", nil)
		if err == nil || !strings.Contains(err.Error(), "machine must be personal|work|remote") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestRemoteTimezoneLocaleValidated(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, filepath.Join(dir, "a.toml"), "machine = \"remote\"\nscope = \"work/acme\"\nmachineName = \"a\"\n")
	for name, env := range map[string]map[string]string{
		"timezone shell": {"ATHOME_TIMEZONE": "UTC; rm -rf /"},
		"timezone quote": {"ATHOME_TIMEZONE": "Europe/A'ms"},
		"locale shell":   {"ATHOME_LOCALE": "en_US$(id)"},
		"locale space":   {"ATHOME_LOCALE": "en US"},
	} {
		_, err := config.Load(config.Options{ConfigPath: p, Home: dir, Env: env})
		key := strings.Fields(name)[0]
		if !errors.Is(err, config.ErrUsage) || !strings.Contains(err.Error(), key) {
			t.Errorf("%s: err = %v, want ErrUsage naming %s", name, err, key)
		}
	}
	env := map[string]string{"ATHOME_TIMEZONE": "America/Port-au-Prince", "ATHOME_LOCALE": "sr_RS@latin"}
	if _, err := config.Load(config.Options{ConfigPath: p, Home: dir, Env: env}); err != nil {
		t.Errorf("valid values refused: %v", err)
	}
}

func TestRemoteNameZeroSaysWhy(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, filepath.Join(dir, "a.toml"), "machine = \"remote\"\nscope = \"work/acme\"\n")
	_, err := config.Load(config.Options{ConfigPath: p, Home: dir, Env: map[string]string{"ATHOME_MACHINE_NAME": "0"}})
	if !errors.Is(err, config.ErrUsage) || !strings.Contains(err.Error(), "canopy reads 0 as unset") {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckNewMachineName(t *testing.T) {
	for _, name := range []string{"a", "acme-dev", "box_1", "00", "10"} {
		if err := config.CheckNewMachineName(name); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
	for _, name := range []string{"", "0", "local", "Box", "-x", "a.b"} {
		if err := config.CheckNewMachineName(name); err == nil {
			t.Errorf("%q accepted", name)
		}
	}
}
