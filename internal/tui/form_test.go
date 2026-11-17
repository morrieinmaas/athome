package tui

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/morrieinmaas/athome/internal/config"
)

func full() map[string]string {
	return map[string]string{
		"machineType": "work", "hostname": "box", "githubHandle": "octo", "githubId": "42",
		"personalName": "Jürgen Müller", "workEmail": "w@x.co", "workName": "山田",
		"sidebizEmail": "s@x.co", "sidebizName": "Side Biz",
		"bitwardenUrl": "https://vault.bitwarden.eu", "useBitwarden": "true",
		"uploadSshKeys": "false", "restoreSecrets": "false",
		"meshProvider": "netbird", "netbirdManagementUrl": "https://nb.example", "nordvpnCountry": "",
		"includeAgents": "false", "agentsRepo": "o/r", "extraPackages": "jq fd",
		"slackDefaultWorkspace": "acme", "slackTeamId": "T01", "matterhornHost": "mm.example", "matterhornUser": "me@x.co",
		"sshKeyAction": "use",
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
	// Both fields are hidden for none, so an env lock must not keep their values.
	v["netbirdManagementUrl"] = "https://nb.example"
	got = newFormState(Seed{Values: v, Locked: map[string]bool{"nordvpnCountry": true, "netbirdManagementUrl": true}}).answers()
	if got["nordvpnCountry"] != "" || got["netbirdManagementUrl"] != "" {
		t.Errorf("locked mesh fields not blanked: %q %q", got["nordvpnCountry"], got["netbirdManagementUrl"])
	}
}

func TestFormBwBackupAnswer(t *testing.T) {
	noKey := func(v map[string]string) map[string]string {
		return newFormState(Seed{Values: v}).answers()
	}
	if got := noKey(map[string]string{})["bwBackupSshKey"]; got != "true" {
		t.Errorf("a generated key with Bitwarden defaults to storing it, got %q", got)
	}
	if got := noKey(map[string]string{"bwBackupSshKey": "false"})["bwBackupSshKey"]; got != "false" {
		t.Errorf("a seeded no is kept, got %q", got)
	}
	for name, v := range map[string]map[string]string{
		"bitwarden off": {"useBitwarden": "false"},
		"key skipped":   {"generateSshKey": "false"},
	} {
		if got, ok := noKey(v)["bwBackupSshKey"]; ok {
			t.Errorf("%s: the question is hidden, so no answer: %q", name, got)
		}
	}
	withKey := func(action string) map[string]string {
		return newFormState(Seed{Values: map[string]string{"sshKeyAction": action}, KeyExists: true}).answers()
	}
	if got, ok := withKey("use")["bwBackupSshKey"]; ok {
		t.Errorf("an existing key kept is not stored again: %q", got)
	}
	if got := withKey("generate")["bwBackupSshKey"]; got != "true" {
		t.Errorf("an existing key replaced by a generated one: %q", got)
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
	for _, page := range st.pages(context.Background()) { // builds with a locked field without panicking
		page()
	}
}

func TestFormDropsGithubIdWhenHandleChanges(t *testing.T) {
	seed := Seed{Values: map[string]string{"githubHandle": "old", "githubId": "42"}}
	st := newFormState(seed)
	if got := st.answers()["githubId"]; got != "42" {
		t.Errorf("unchanged handle: id = %q, want 42", got)
	}
	*st.v["githubHandle"] = "new"
	if got := st.answers()["githubId"]; got != "" {
		t.Errorf("changed handle kept the old id %q", got)
	}
	seed.Locked = map[string]bool{"githubId": true}
	st = newFormState(seed)
	*st.v["githubHandle"] = "new"
	if got := st.answers()["githubId"]; got != "42" {
		t.Errorf("a locked id must stay as seeded, got %q", got)
	}
}

// The key is chezmoi's hostname's, so answering another hostname leaves the ssh choice alone.
func TestFormHostnameAnswerDoesNotChangeKeyExistence(t *testing.T) {
	st := newFormState(Seed{Values: map[string]string{"hostname": "old"}, KeyExists: true})
	*st.v["hostname"] = "new"
	if got := st.answers(); got["sshKeyAction"] != "use" || got["generateSshKey"] != "" || got["hostname"] != "new" {
		t.Errorf("want sshKeyAction=use and hostname=new: %v", got)
	}
}

func TestFormUseBitwardenOffKeepsUrlUnansweredAndSkipsRestore(t *testing.T) {
	v := full()
	v["useBitwarden"], v["bitwardenUrl"], v["restoreSecrets"] = "false", "https://vault.bitwarden.eu", "true"
	got := newFormState(Seed{Values: v}).answers()
	if _, ok := got["bitwardenUrl"]; ok || got["restoreSecrets"] != "false" {
		t.Errorf("bitwardenUrl answered=%v restoreSecrets=%q, want unanswered and false", ok, got["restoreSecrets"])
	}
	v["useBitwarden"] = "true"
	got = newFormState(Seed{Values: v}).answers()
	if got["bitwardenUrl"] != "https://vault.bitwarden.eu" || got["restoreSecrets"] != "true" {
		t.Errorf("with Bitwarden on both stay: %q %q", got["bitwardenUrl"], got["restoreSecrets"])
	}
}

// keysForm is the keys page as a model that quits on submit, as huh's own runner arranges.
func keysForm(useBitwarden string) *huh.Form {
	st := newFormState(Seed{Values: map[string]string{"useBitwarden": useBitwarden}})
	f := st.pages(context.Background())[2]().WithKeyMap(quitKeys)
	f.SubmitCmd, f.CancelCmd = tea.Quit, tea.Quit
	return f
}

// keysPage runs the keys page under teatest with useBitwarden preset and presses
// enter through its first group (three questions).
func keysPage(t *testing.T, useBitwarden string) *teatest.TestModel {
	t.Helper()
	tm := teatest.NewTestModel(t, keysForm(useBitwarden), teatest.WithInitialTermSize(80, 24))
	waitOutput(t, tm, "Use Bitwarden") // keys sent before the first frame can race the form's Init
	for range 3 {
		tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	}
	return tm
}

func TestFormKeysPageLabelAndBitwardenHiding(t *testing.T) {
	on := keysPage(t, "true")
	var seen string
	teatest.WaitFor(t, on.Output(), func(b []byte) bool {
		seen = string(b)
		return strings.Contains(seen, "Bitwarden server URL")
	}, teatest.WithDuration(5*time.Second), teatest.WithCheckInterval(10*time.Millisecond))
	f := keysForm("true")
	f.Init()
	f.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if v := f.View(); !strings.Contains(v, "Use Bitwarden (rbw) for secrets sync?") || strings.Contains(v, "Store the ssh key") {
		t.Errorf("wrong label: %q", v)
	}

	off := keysPage(t, "false") // the URL and restore questions are hidden: the page ends after three enters
	off.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
	out, _ := io.ReadAll(off.FinalOutput(t))
	if strings.Contains(string(out), "Bitwarden server URL") || strings.Contains(string(out), "Restore secrets") {
		t.Errorf("with Bitwarden off the URL and restore questions are hidden: %q", out)
	}
}

// A page narrower than a phone still renders its fields, within the width.
func TestFormPageAtWidth40(t *testing.T) {
	page := func() *huh.Form {
		f := newFormState(Seed{Values: map[string]string{}}).pages(context.Background())[0]().WithKeyMap(quitKeys)
		f.SubmitCmd, f.CancelCmd = tea.Quit, tea.Quit
		return f
	}
	tm := teatest.NewTestModel(t, page(), teatest.WithInitialTermSize(40, 24))
	waitOutput(t, tm, "Hostname")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))

	g := page()
	g.Init()
	g.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	for _, line := range strings.Split(g.View(), "\n") {
		if w := lipgloss.Width(line); w > 40 {
			t.Errorf("line is %d wide: %q", w, line)
		}
	}
}
