package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// Choosing reupload means upload: the answer is true whatever uploadSshKeys was
// seeded as.
func TestFormReuploadImpliesUpload(t *testing.T) {
	st := newFormState(Seed{Values: map[string]string{"uploadSshKeys": "false"}, KeyExists: true})
	st.ssh = "reupload"
	if got := st.answers(); got["sshKeyAction"] != "reupload" || got["uploadSshKeys"] != "true" {
		t.Errorf("reupload answers = %v, want uploadSshKeys=true", got)
	}
	st.ssh = "use"
	if got := st.answers(); got["uploadSshKeys"] != "false" {
		t.Errorf("use keeps the upload answer, got %q", got["uploadSshKeys"])
	}
}

// pump feeds msg to the form and every message its commands produce.
func pump(f *huh.Form, msg tea.Msg) {
	var feed func(tea.Msg)
	feed = func(m tea.Msg) {
		if b, ok := m.(tea.BatchMsg); ok {
			for _, c := range b {
				if c != nil {
					feed(c())
				}
			}
			return
		}
		if m == nil {
			return
		}
		_, cmd := f.Update(m)
		if cmd != nil {
			feed(cmd())
		}
	}
	feed(msg)
}

// With reupload chosen the upload question is skipped: the page ends after the
// ssh choice. With use chosen it is asked.
func TestFormReuploadHidesUploadQuestion(t *testing.T) {
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	for _, c := range []struct {
		ssh      string
		wantDone bool
	}{{"reupload", true}, {"use", false}} {
		st := newFormState(Seed{Values: map[string]string{"useBitwarden": "false"}, KeyExists: true})
		st.ssh = c.ssh
		f := st.pages(context.Background())[2]().WithKeyMap(quitKeys)
		f.SubmitCmd, f.CancelCmd = tea.Quit, tea.Quit
		f.Init()
		pump(f, tea.WindowSizeMsg{Width: 80, Height: 24})
		pump(f, enter) // useBitwarden
		pump(f, enter) // ssh choice
		if done := f.State == huh.StateCompleted; done != c.wantDone {
			t.Errorf("ssh=%s: page completed = %v, want %v\n%s", c.ssh, done, c.wantDone, f.View())
		}
		if !c.wantDone && !strings.Contains(f.View(), "Upload keys to GitHub?") {
			t.Errorf("ssh=%s: the upload question should show", c.ssh)
		}
	}
}
