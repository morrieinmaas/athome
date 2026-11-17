package tui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

func runHomeModel(t *testing.T, info HomeInfo, keys ...tea.KeyMsg) *homeModel {
	t.Helper()
	tm := teatest.NewTestModel(t, &homeModel{info: info}, teatest.WithInitialTermSize(80, 24))
	waitOutput(t, tm, "athome")
	for _, k := range keys {
		tm.Send(k)
	}
	final := tm.FinalModel(t, teatest.WithFinalTimeout(5*time.Second)).(*homeModel)
	return final
}

func TestHomeSetUp(t *testing.T) {
	info := HomeInfo{
		Version: "v1.2.3", Commit: "abc123", Repo: "git@x:me/dots.git",
		MachineType: "personal", Hostname: "box", Handle: "octo", SetUp: true,
	}
	tm := teatest.NewTestModel(t, &homeModel{info: info}, teatest.WithInitialTermSize(80, 24))
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		s := string(b)
		for _, w := range []string{"v1.2.3", "abc123", "git@x:me/dots.git", "personal", "box", "octo"} {
			if !strings.Contains(s, w) {
				return false
			}
		}
		return !strings.Contains(s, "not set up")
	}, teatest.WithDuration(5*time.Second))
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	if got := tm.FinalModel(t, teatest.WithFinalTimeout(5*time.Second)).(*homeModel).choice; got != "init" {
		t.Errorf("choice = %q, want init", got)
	}
}

func TestHomeNotSetUpQuit(t *testing.T) {
	tm := teatest.NewTestModel(t, &homeModel{info: HomeInfo{Version: "v1", Commit: "c", Repo: "r"}}, teatest.WithInitialTermSize(80, 24))
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool { return strings.Contains(string(b), "not set up") },
		teatest.WithDuration(5*time.Second))
	tm.Type("j")
	tm.Type("j")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	if got := tm.FinalModel(t, teatest.WithFinalTimeout(5*time.Second)).(*homeModel).choice; got != "quit" {
		t.Errorf("choice = %q, want quit", got)
	}
}

func TestHomeQuitKeys(t *testing.T) {
	for _, k := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("q")}, {Type: tea.KeyCtrlC}, {Type: tea.KeyEsc}} {
		m := runHomeModel(t, HomeInfo{}, k)
		if m.choice != "quit" {
			t.Errorf("%v: choice = %q, want quit", k, m.choice)
		}
	}
}

// RunHome itself: input in, choice out.
func TestRunHomeReturnsTheChoice(t *testing.T) {
	var out bytes.Buffer
	got, err := RunHome(strings.NewReader("\r"), &out, HomeInfo{Version: "v1"})
	if err != nil || got != "init" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestHomeMenuOffersUpdate(t *testing.T) {
	m := runHomeModel(t, HomeInfo{}, tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyEnter})
	if m.choice != "update" {
		t.Errorf("choice = %q, want update", m.choice)
	}
}

func TestHomeShowsKeysReminder(t *testing.T) {
	info := HomeInfo{Version: "v1", SetUp: true, KeysReminder: "keys: last rotated 120 days ago; run athome keys rotate --laptop"}
	tm := teatest.NewTestModel(t, &homeModel{info: info}, teatest.WithInitialTermSize(80, 24))
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return strings.Contains(string(b), "last rotated 120 days ago")
	}, teatest.WithDuration(5*time.Second))
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.FinalModel(t, teatest.WithFinalTimeout(5*time.Second))
}
