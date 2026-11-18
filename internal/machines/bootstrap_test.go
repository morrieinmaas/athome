package machines

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func laptopData() map[string]any {
	return map[string]any{
		"githubHandle": "morrie", "githubId": "42", "personalName": "Morrie",
		"workEmail": "m@acme.example", "workName": "Morrie A", "sidebizEmail": "m@side.example", "sidebizName": "Morrie S",
		"includeAgents": true, "agentsRepo": "", "meshProvider": "meshnet", "netbirdManagementUrl": "",
		"bitwardenUrl": "https://bw.example", "slackTeamId": "T1",
	}
}

func TestBootstrapTOMLWork(t *testing.T) {
	m := Machine{Name: "acme-dev", Scope: "work/acme"}
	got := BootstrapTOML(laptopData(), m, "Europe/Amsterdam", "en_US.UTF-8")
	for _, want := range []string{
		`machine = "remote"`, `scope = "work/acme"`, `machineName = "acme-dev"`, `githubHandle = "morrie"`, `githubId = "42"`,
		`personalName = "Morrie"`, `workEmail = "m@acme.example"`, `workName = "Morrie A"`, `includeAgents = "true"`,
		`meshProvider = "netbird"`, `generateSshKey = "false"`, `useBitwarden = "false"`,
		`timezone = "Europe/Amsterdam"`, `locale = "en_US.UTF-8"`,
	} {
		if !strings.Contains(got, want+"\n") {
			t.Errorf("missing %s in:\n%s", want, got)
		}
	}
	for _, not := range []string{"sidebiz", "agentsRepo", "netbirdManagementUrl", "bitwardenUrl", "slack"} {
		if strings.Contains(got, not) {
			t.Errorf("%s must not be in:\n%s", not, got)
		}
	}
}

func TestBootstrapTOMLOtherScopes(t *testing.T) {
	side := BootstrapTOML(laptopData(), Machine{Name: "s", Scope: "sidebiz/x"}, "", "")
	if !strings.Contains(side, `sidebizEmail = "m@side.example"`) || !strings.Contains(side, `sidebizName = "Morrie S"`) || strings.Contains(side, "workEmail") {
		t.Errorf("sidebiz:\n%s", side)
	}
	if strings.Contains(side, "timezone") || strings.Contains(side, "locale") {
		t.Errorf("empty values written:\n%s", side)
	}
	pers := BootstrapTOML(laptopData(), Machine{Name: "p", Scope: "personal/y"}, "", "")
	if strings.Contains(pers, "workEmail") || strings.Contains(pers, "sidebizEmail") || !strings.Contains(pers, `scope = "personal/y"`) {
		t.Errorf("personal:\n%s", pers)
	}
	if got := BootstrapTOML(nil, Machine{Name: "p", Scope: "personal/y"}, "", ""); !strings.Contains(got, `machine = "remote"`) {
		t.Errorf("nil data:\n%s", got)
	}
}

func TestLaptopTimezone(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "localtime")
	os.Symlink("/var/db/timezone/zoneinfo/Europe/Amsterdam", link)
	old := localtimePath
	localtimePath = link
	t.Cleanup(func() { localtimePath = old })
	if got := laptopTimezone(); got != "Europe/Amsterdam" {
		t.Errorf("timezone = %q", got)
	}
	localtimePath = filepath.Join(dir, "missing")
	if got := laptopTimezone(); got != "" {
		t.Errorf("missing link: %q", got)
	}
}

func TestBootstrapUsesStoredDataTimezoneAndLang(t *testing.T) {
	w, d, _ := newWorld(t)
	os.MkdirAll(filepath.Join(d.Home, ".config", "chezmoi"), 0o755)
	os.WriteFile(filepath.Join(d.Home, ".config", "chezmoi", "chezmoi.toml"), []byte("[data]\ngithubHandle = \"morrie\"\nworkName = \"Morrie A\"\n"), 0o644)
	link := filepath.Join(t.TempDir(), "localtime")
	os.Symlink("/usr/share/zoneinfo/UTC", link)
	old := localtimePath
	localtimePath = link
	t.Cleanup(func() { localtimePath = old })
	t.Setenv("LANG", "nl_NL.UTF-8")
	m := acme()
	if err := bootstrapBox(t.Context(), d, m, true, d.Out); err != nil {
		t.Fatal(err)
	}
	got := w.boxes["acme-dev"].bootstrap
	for _, want := range []string{`githubHandle = "morrie"`, `workName = "Morrie A"`, `timezone = "UTC"`, `locale = "nl_NL.UTF-8"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s:\n%s", want, got)
		}
	}
}

func TestBootstrapDoneComparesVersionAndAnswers(t *testing.T) {
	w, d, _ := newWorld(t)
	m := acme()
	if ok, err := bootstrapDone(t.Context(), d, m); ok || err != nil {
		t.Fatalf("fresh box: %v %v", ok, err)
	}
	if err := bootstrapBox(t.Context(), d, m, true, d.Out); err != nil {
		t.Fatal(err)
	}
	if ok, err := bootstrapDone(t.Context(), d, m); !ok || err != nil {
		t.Fatalf("after bootstrap: %v %v", ok, err)
	}
	w.boxes["acme-dev"].bootstrap = "machine = \"remote\"\n# stale\n"
	if ok, _ := bootstrapDone(t.Context(), d, m); ok {
		t.Fatal("changed answers still count as done")
	}
	m.Athome = "v0.5.0"
	w.boxes["acme-dev"].bootstrap = d.bootstrapTOML(m)
	if ok, _ := bootstrapDone(t.Context(), d, m); ok {
		t.Fatal("another pinned tag counts as done")
	}
	w.box("acme-dev").down = true
	if _, err := bootstrapDone(t.Context(), d, m); err == nil {
		t.Fatal("unreachable box is not an error")
	}
}

func TestBootstrapTOMLForcesNetbirdWhateverTheLaptopUses(t *testing.T) {
	for _, laptop := range []any{"meshnet", "none", nil, "netbird"} {
		data := laptopData()
		data["meshProvider"] = laptop
		got := BootstrapTOML(data, Machine{Name: "b", Scope: "work/acme"}, "", "")
		if !strings.Contains(got, `meshProvider = "netbird"`+"\n") || strings.Count(got, "meshProvider") != 1 {
			t.Errorf("laptop %v:\n%s", laptop, got)
		}
	}
}
