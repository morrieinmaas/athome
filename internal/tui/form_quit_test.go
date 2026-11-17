package tui

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestReviewPageQuitKeys(t *testing.T) {
	for k, want := range map[string]string{"q": "quit", "ctrl+c": "quit", "enter": "apply", "b": "back", "esc": ""} {
		m := &reviewModel{}
		var msg tea.KeyMsg
		switch k {
		case "ctrl+c":
			msg = tea.KeyMsg{Type: tea.KeyCtrlC}
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		m.Update(msg)
		if m.choice != want {
			t.Errorf("key %q: choice %q, want %q", k, m.choice, want)
		}
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
	// huh skips groups that hold only read-only notes, except a group of a single
	// note, which it shows: the upload and store-the-key questions are each one.
	waitFor("Upload keys to GitHub?")
	pw2.WriteString("\r")
	waitFor("Store the ssh key in Bitwarden?")
	pw2.WriteString("\r")
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

// A cancel on a huh page quits, and RunForm returns only once bubbletea's read
// loop is done with the input, so closing it then is no race (run with -race).
func TestFormCancelOnPageIsQuit(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pw.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := &syncBuf{}
	done := make(chan error, 1)
	go func() {
		_, err := RunForm(ctx, pr, out, Seed{Values: map[string]string{}})
		done <- err
	}()
	for deadline := time.Now().Add(10 * time.Second); !strings.Contains(out.String(), "Hostname"); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for the first page in %q", out.String())
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, ErrQuit) {
			t.Fatalf("err = %v, want ErrQuit", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunForm did not return after the context was cancelled")
	}
	pr.Close()
}

// esc clears a select's filter, so it must not quit the form.
func TestEscDoesNotQuitForm(t *testing.T) {
	keys := quitKeys.Quit.Keys()
	if len(keys) != 1 || keys[0] != "ctrl+c" {
		t.Errorf("quit keys = %v, want only ctrl+c", keys)
	}
}
