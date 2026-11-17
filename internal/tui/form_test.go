package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/morrieinmaas/athome/internal/config"
)

func full() map[string]string {
	return map[string]string{
		"machineType": "work", "hostname": "box", "githubHandle": "octo", "githubId": "42",
		"personalName": "Jürgen Müller", "workEmail": "w@x.co", "workName": "山田",
		"sidebizEmail": "s@x.co", "sidebizName": "Side Biz",
		"bitwardenUrl": "https://vault.bitwarden.eu", "useBitwarden": "false",
		"uploadSshKeys": "false", "restoreSecrets": "false",
		"meshProvider": "netbird", "netbirdManagementUrl": "https://nb.example", "nordvpnCountry": "",
		"includeAgents": "false", "agentsRepo": "o/r", "extraPackages": "jq fd",
		"slackDefaultWorkspace": "acme", "slackTeamId": "T01", "matterhornHost": "mm.example", "matterhornUser": "me@x.co",
		"sshKeyAction": "reupload",
	}
}

// FormMatchesBootstrapToml: the form's answers equal config.Load on a bootstrap.toml with the same values.
func TestFormAnswersMatchBootstrapToml(t *testing.T) {
	vals := full()
	st := newFormState(Seed{Values: vals, KeyExists: true})
	got := st.answers()

	var b strings.Builder
	for k, v := range vals {
		if k == "machineType" || k == "hostname" || k == "includeAgents" || k == "uploadSshKeys" || k == "restoreSecrets" {
			continue
		}
		b.WriteString(k + " = \"" + v + "\"\n")
	}
	b.WriteString("machine = \"work\"\nincludeAgents = false\n")
	dir := t.TempDir()
	p := filepath.Join(dir, "b.toml")
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Options{ConfigPath: p, Home: dir, Env: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range cfg.Data {
		if want != "" && got[k] != want {
			t.Errorf("%s: form %q, bootstrap.toml %q", k, got[k], want)
		}
		if want == "" && k != "useBitwarden" && k != "generateSshKey" && got[k] != "" {
			t.Errorf("%s: form %q, want empty", k, got[k])
		}
	}
	for _, k := range []string{"machineType", "hostname", "includeAgents", "uploadSshKeys", "restoreSecrets"} {
		if got[k] != vals[k] {
			t.Errorf("%s=%q want %q", k, got[k], vals[k])
		}
	}
	if _, ok := got["generateSshKey"]; ok {
		t.Error("generateSshKey set although a key exists")
	}
}

func TestFormMeshNoneBlanksBoth(t *testing.T) {
	v := full()
	v["meshProvider"], v["nordvpnCountry"] = "none", "Netherlands"
	got := newFormState(Seed{Values: v}).answers()
	if got["nordvpnCountry"] != "" || got["netbirdManagementUrl"] != "" {
		t.Errorf("mesh fields not blanked: %q %q", got["nordvpnCountry"], got["netbirdManagementUrl"])
	}
}

func TestFormSshKeyMapping(t *testing.T) {
	g := newFormState(Seed{Values: map[string]string{}}).answers()
	if g["generateSshKey"] != "true" || g["sshKeyAction"] != "" {
		t.Errorf("no key, default: %v", g)
	}
	g = newFormState(Seed{Values: map[string]string{"generateSshKey": "false"}}).answers()
	if g["generateSshKey"] != "false" {
		t.Errorf("no key, skip: %v", g)
	}
	g = newFormState(Seed{Values: map[string]string{}, KeyExists: true}).answers()
	if g["sshKeyAction"] != "use" || g["generateSshKey"] != "" {
		t.Errorf("key exists default: %v", g)
	}
}

func TestFormSeedLockedKeptAndDefaults(t *testing.T) {
	st := newFormState(Seed{
		Values: map[string]string{"githubHandle": "fixed", "meshProvider": "meshnet"},
		Locked: map[string]bool{"githubHandle": true},
	})
	*st.v["githubHandle"] = "tampered"
	got := st.answers()
	if got["githubHandle"] != "fixed" {
		t.Errorf("locked value changed: %q", got["githubHandle"])
	}
	if got["machineType"] != "personal" || got["useBitwarden"] != "true" {
		t.Errorf("defaults missing: %v", got)
	}
	st.pages(context.Background()) // builds with a locked field without panicking
}

func TestFormReviewKeys(t *testing.T) {
	for key, want := range map[string]string{"enter": "apply", "b": "back", "q": "quit"} {
		m := &reviewModel{}
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
		if key == "enter" {
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		}
		m.Update(msg)
		if m.choice != want {
			t.Errorf("%s -> %q want %q", key, m.choice, want)
		}
	}
	m := &reviewModel{}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if m.choice != "quit" {
		t.Error("ctrl-c does not quit")
	}
}

func TestFormQuitAtFirstPage(t *testing.T) {
	_, err := RunForm(context.Background(), strings.NewReader("\x03"), &strings.Builder{}, Seed{Values: map[string]string{}})
	if !errors.Is(err, ErrQuit) {
		t.Fatalf("err=%v want ErrQuit", err)
	}
}
