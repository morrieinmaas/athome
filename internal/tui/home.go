package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type HomeInfo struct {
	Version, Commit, Repo, MachineType, Hostname, Handle string
	SetUp                                                bool
}

var homeChoices = []string{"init", "quit"}

type homeModel struct {
	info   HomeInfo
	cursor int
	choice string
}

func (m *homeModel) Init() tea.Cmd { return nil }

func (m *homeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch k.String() {
	case "up", "k":
		m.cursor = (m.cursor + len(homeChoices) - 1) % len(homeChoices)
	case "down", "j", "tab":
		m.cursor = (m.cursor + 1) % len(homeChoices)
	case "enter":
		m.choice = homeChoices[m.cursor]
		return m, tea.Quit
	case "q", "ctrl+c", "esc":
		m.choice = "quit"
		return m, tea.Quit
	}
	return m, nil
}

func (m *homeModel) View() string {
	var b strings.Builder
	i := m.info
	fmt.Fprintf(&b, "athome %s (%s)\n", i.Version, i.Commit)
	fmt.Fprintf(&b, "repo: %s\n\n", i.Repo)
	if i.SetUp {
		fmt.Fprintf(&b, "machine type: %s\nhostname: %s\ngithub handle: %s\n\n", i.MachineType, i.Hostname, i.Handle)
	} else {
		b.WriteString("not set up\n\n")
	}
	for n, c := range homeChoices {
		cur := "  "
		if n == m.cursor {
			cur = "> "
		}
		b.WriteString(cur + c + "\n")
	}
	return b.String()
}

// RunHome shows the home screen and returns "init" or "quit".
func RunHome(in io.Reader, out io.Writer, info HomeInfo) (string, error) {
	m := &homeModel{info: info}
	if _, err := runProgram(context.Background(), m, tea.WithInput(in), tea.WithOutput(out)); err != nil {
		if errors.Is(err, tea.ErrInterrupted) {
			return "quit", nil
		}
		return "", err
	}
	if m.choice == "" {
		m.choice = "quit"
	}
	return m.choice, nil
}
