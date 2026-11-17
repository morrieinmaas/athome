package prompt

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func ask(in string) (*Asker, *bytes.Buffer) {
	var out bytes.Buffer
	return New(strings.NewReader(in), &out), &out
}

func TestConfirm(t *testing.T) {
	for _, c := range []struct {
		name, in  string
		def, want bool
	}{
		{"empty takes default true", "\n", true, true},
		{"confirm EOF takes default", "", true, true},
		{"EOF default false", "", false, false},
		{"spaces n", " n \n", true, false},
		{"upper Y", "Y\n", false, true},
		{"upper YES", "YES\n", false, true},
		{"no", "no\n", true, false},
		{"invalid re-asks", "maybe\ny\n", false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, _ := ask(c.in)
			if got := a.Confirm("symlink it?", c.def); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestConfirmPromptMarks(t *testing.T) {
	a, out := ask("\n")
	a.Confirm("q?", true)
	if !strings.Contains(out.String(), "[Y/n]") {
		t.Errorf("out %q", out.String())
	}
	a, out = ask("\n")
	a.Confirm("q?", false)
	if !strings.Contains(out.String(), "[y/N]") {
		t.Errorf("out %q", out.String())
	}
}

func TestChoose(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"first letter", "g\n", "generate"},
		{"choose invalid re-asks", "x\nreupload\n", "reupload"},
		{"empty is default", "\n", "use"},
		{"EOF is default", "", "use"},
		{"case-insensitive word", "GENERATE\n", "generate"},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, out := ask(c.in)
			if got := a.Choose("ssh key found", "use", "use", "generate", "reupload"); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
			if !strings.Contains(out.String(), "[use/g/r]") {
				t.Errorf("out %q", out.String())
			}
			if c.name == "choose invalid re-asks" && strings.Count(out.String(), "ssh key found") != 2 {
				t.Errorf("prompt should print twice: %q", out.String())
			}
		})
	}
}

func TestString(t *testing.T) {
	a, _ := ask("\nalice\n")
	if got := a.String("GitHub username: ", true); got != "alice" {
		t.Errorf("got %q", got)
	}
	a, _ = ask("")
	if got := a.String("GitHub username: ", true); got != "" {
		t.Errorf("EOF got %q", got)
	}
	a, _ = ask("\n")
	if got := a.String("url: ", false); got != "" {
		t.Errorf("optional got %q", got)
	}
}

func TestSuccessiveQuestionsKeepBufferedInput(t *testing.T) {
	a, _ := ask("n\nbob\n")
	if a.Confirm("a?", true) {
		t.Error("want false")
	}
	if got := a.String("b: ", true); got != "bob" {
		t.Errorf("got %q", got)
	}
}

func TestSayWritesToTheAskersOutput(t *testing.T) {
	a, out := ask("")
	a.Say("menu %s\n", "line")
	if got := out.String(); got != "menu line\n" {
		t.Fatalf("Say wrote %q", got)
	}
}

// Closing Done during a blocked read calls OnCancel; Confirm must not fall back
// to its default.
func TestCancelAtPrompt(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	var out bytes.Buffer
	a := New(pr, &out)
	done := make(chan struct{})
	called := make(chan struct{})
	a.Done = done
	a.OnCancel = func() {
		close(called)
		panic("exit")
	}
	returned := make(chan bool, 1)
	go func() {
		defer func() { recover() }()
		returned <- a.Confirm("go?", true)
	}()
	close(done)
	select {
	case <-called:
	case got := <-returned:
		t.Fatalf("Confirm returned %v without calling OnCancel", got)
	}
	select {
	case got := <-returned:
		t.Errorf("Confirm returned %v after cancel, want it not to return", got)
	default:
	}
}

// A Done that is already closed cancels before any input is read.
func TestCancelBeforePrompt(t *testing.T) {
	a, _ := ask("y\n")
	done := make(chan struct{})
	close(done)
	a.Done = done
	cancelled := false
	a.OnCancel = func() { cancelled = true }
	if _, ok := a.line("q "); ok || !cancelled {
		t.Errorf("ok=%v cancelled=%v, want a cancelled read", ok, cancelled)
	}
}
