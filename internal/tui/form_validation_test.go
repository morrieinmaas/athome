package tui

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// A bad email keeps the identity page up; the form must not move on to later pages.
func TestFormBadEmailDoesNotAdvance(t *testing.T) {
	seed := lockedSeed()
	seed.Locked["workEmail"] = false
	seed.Values["workEmail"] = ""
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	out := &syncBuf{}
	done := make(chan error, 1)
	go func() {
		_, err := RunForm(context.Background(), pr, out, seed)
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
	waitFor("Work git email")
	pw.WriteString("bad\r")
	waitFor("not an email address")
	time.Sleep(300 * time.Millisecond) // a wrongly advancing form would show the later pages by now
	if s := out.String(); strings.Contains(s, "Network") || strings.Contains(s, "enter apply") {
		t.Errorf("the form advanced past a bad email: %q", s)
	}
	pw.WriteString("\x03")
	select {
	case err := <-done:
		if !errors.Is(err, ErrQuit) {
			t.Fatalf("err = %v, want ErrQuit", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunForm did not return after ctrl-c")
	}
	pr.Close()
	pw.Close()
}
