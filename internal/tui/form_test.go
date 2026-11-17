package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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
	st := newFormState(Seed{Values: vals, KeyExists: func(string) bool { return true }})
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
	g = newFormState(Seed{Values: map[string]string{}, KeyExists: func(string) bool { return true }}).answers()
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

// A cancelled context (SIGTERM) ends the form as a quit, not as huh's timeout.
func TestFormCancelledContextIsQuit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := RunForm(ctx, strings.NewReader(""), &strings.Builder{}, Seed{Values: map[string]string{}})
	if !errors.Is(err, ErrQuit) {
		t.Fatalf("err = %v, want ErrQuit", err)
	}
}

type syncBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// lockedSeed locks every field, so each page holds read-only notes only.
func lockedSeed() Seed {
	vals := map[string]string{"generateSshKey": "true", "meshProvider": "none"}
	locked := map[string]bool{"generateSshKey": true}
	for _, k := range formKeys {
		locked[k] = true
		if _, ok := vals[k]; !ok {
			vals[k] = "v"
		}
	}
	vals["hostname"] = "box"
	return Seed{Values: vals, Locked: locked}
}

// runLockedToReview feeds enters page by page until the review page shows.
func runLockedToReview(t *testing.T, ctx context.Context) (*syncBuf, *os.File, chan error) {
	t.Helper()
	pr, pw2, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	out := &syncBuf{}
	done := make(chan error, 1)
	returned := make(chan struct{})
	t.Cleanup(func() { // close the pipe only once the form is no longer reading it
		select {
		case <-returned:
		case <-time.After(5 * time.Second):
		}
		pr.Close()
		pw2.Close()
	})
	go func() {
		_, err := RunForm(ctx, pr, out, lockedSeed())
		close(returned)
		done <- err
	}()
	waitFor := func(want string) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); !strings.Contains(out.String(), want); time.Sleep(10 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %q in %q", want, out.String())
			}
		}
	}
	// huh skips groups that hold only read-only notes, so only the last form
	// shows a page to confirm.
	waitFor("Network")
	pw2.WriteString("\r")
	waitFor("enter apply, b back to edit, q quit")
	return out, pw2, done
}

func TestFormReviewQuitKey(t *testing.T) {
	_, pw, done := runLockedToReview(t, context.Background())
	pw.WriteString("q")
	select {
	case err := <-done:
		if !errors.Is(err, ErrQuit) {
			t.Fatalf("err = %v, want ErrQuit", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunForm did not return after q on the review page")
	}
}

func TestFormCancelOnReviewIsQuit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, _, done := runLockedToReview(t, ctx)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, ErrQuit) {
			t.Fatalf("err = %v, want ErrQuit", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunForm did not return after the context was cancelled")
	}
}

// Whether a key exists follows the hostname the form answered, not the seeded one.
func TestFormKeyExistsFollowsAnsweredHostname(t *testing.T) {
	seed := Seed{
		Values:    map[string]string{"hostname": "old"},
		KeyExists: func(h string) bool { return h == "new" },
	}
	st := newFormState(seed)
	if got := st.answers(); got["generateSshKey"] != "true" || got["sshKeyAction"] != "" {
		t.Errorf("old hostname has no key: %v", got)
	}
	*st.v["hostname"] = "new"
	st.syncSSH()
	got := st.answers()
	if got["sshKeyAction"] != "use" || got["generateSshKey"] != "" {
		t.Errorf("new hostname has a key, want sshKeyAction=use and no generateSshKey: %v", got)
	}
	*st.v["hostname"] = "old"
	st.syncSSH()
	if got := st.answers(); got["generateSshKey"] != "true" || got["sshKeyAction"] != "" {
		t.Errorf("back to the old hostname: %v", got)
	}
}
