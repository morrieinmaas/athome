package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/morrieinmaas/athome/internal/machines"
)

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func driveMachines(info MachinesInfo, keys ...string) *machinesModel {
	m := &machinesModel{info: info}
	for _, k := range keys {
		m.Update(keyMsg(k))
	}
	return m
}

var twoBoxes = MachinesInfo{Listing: "a  work/a\nb  work/b\n", Names: []string{"a", "b"}}

func TestMachinesHomeEntryOrder(t *testing.T) {
	if homeChoices[len(homeChoices)-2] != "machines" || homeChoices[len(homeChoices)-1] != "quit" {
		t.Errorf("homeChoices = %v", homeChoices)
	}
}

func TestMachinesActions(t *testing.T) {
	cases := map[string]struct {
		keys []string
		want MachinesResult
	}{
		"add":      {[]string{"enter"}, MachinesResult{Action: "add"}},
		"sync":     {[]string{"down", "enter", "down", "enter"}, MachinesResult{Action: "sync", Name: "b"}},
		"sync all": {[]string{"down", "down", "enter"}, MachinesResult{Action: "sync all"}},
		"back":     {[]string{"q"}, MachinesResult{Action: "back"}},
	}
	for name, c := range cases {
		if got := driveMachines(twoBoxes, c.keys...).result; got.Action != c.want.Action || got.Name != c.want.Name {
			t.Errorf("%s: got %+v want %+v", name, got, c.want)
		}
	}
}

func TestMachinesRemoveConfirmDefaultsNo(t *testing.T) {
	toRemove := []string{"down", "down", "down", "enter", "enter"} // remove, pick a
	m := driveMachines(twoBoxes, append(toRemove, "enter")...)
	if m.result.Action != "" || !strings.Contains(driveMachines(twoBoxes, toRemove...).View(), "remove a? This revokes its GitHub keys") {
		t.Errorf("enter must decline: result %+v", m.result)
	}
	if got := driveMachines(twoBoxes, append(toRemove, "y")...).result; got.Action != "remove" || got.Name != "a" {
		t.Errorf("y: got %+v", got)
	}
}

func TestMachinesNoneToPick(t *testing.T) {
	m := driveMachines(MachinesInfo{Listing: "no machines\n"}, "down", "enter")
	if m.result.Action != "" || !strings.Contains(m.View(), "no machines") {
		t.Errorf("sync with an empty inventory must stay: %+v", m.result)
	}
}

func TestMachineFieldValidation(t *testing.T) {
	bad := map[string]error{
		"scope":  fieldValidator(func(m *machines.Machine, v string) { m.Scope = v })("elsewhere/x"),
		"name":   fieldValidator(func(m *machines.Machine, v string) { m.Name = v })("local"),
		"target": fieldValidator(func(m *machines.Machine, v string) { m.Target = v })("a b"),
		"port":   validPort("70000"),
	}
	for f, err := range bad {
		if err == nil || !strings.Contains(err.Error(), f) {
			t.Errorf("%s: err = %v", f, err)
		}
	}
	if err := validPort("abc"); err == nil {
		t.Error("port abc passed")
	}
	if err := fieldValidator(func(m *machines.Machine, v string) { m.Scope = v })("work/sub/x"); err != nil {
		t.Errorf("good scope: %v", err)
	}
}

func TestAddValuesMachine(t *testing.T) {
	a := &addValues{name: " b1 ", scope: "work/x", target: "h", user: "u", port: "2222", forward: "true", tag: "v1.2.3"}
	m := a.machine()
	if m.Name != "b1" || m.Port != 2222 || !m.ForwardAgent || m.Athome != "v1.2.3" {
		t.Errorf("%+v", m)
	}
}
