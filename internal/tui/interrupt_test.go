package tui

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// An external SIGINT makes p.Run return tea.ErrInterrupted; that is a quit.
func interrupted(t *testing.T) {
	t.Helper()
	old := runProgram
	runProgram = func(context.Context, tea.Model, ...tea.ProgramOption) (tea.Model, error) {
		return nil, tea.ErrInterrupted
	}
	t.Cleanup(func() { runProgram = old })
}

func TestFormInterruptedIsQuit(t *testing.T) {
	interrupted(t)
	_, err := RunForm(context.Background(), strings.NewReader(""), io.Discard, Seed{Values: map[string]string{}})
	if !errors.Is(err, ErrQuit) {
		t.Fatalf("err = %v, want ErrQuit", err)
	}
}

func TestHomeInterruptedIsQuit(t *testing.T) {
	interrupted(t)
	got, err := RunHome(strings.NewReader(""), io.Discard, HomeInfo{})
	if err != nil || got != "quit" {
		t.Fatalf("got %q, %v; want quit, nil", got, err)
	}
}
