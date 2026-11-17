// Package prompt asks the interactive questions of athome init.
package prompt

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Asker reads answers from one buffered reader, so successive questions do not
// lose input.
type Asker struct {
	in  *bufio.Reader
	out io.Writer

	// Done, when set, cancels a prompt that is waiting for input: line then calls
	// OnCancel, which should not return (it exits the process). If it does return,
	// line reports no input, as at EOF.
	Done     <-chan struct{}
	OnCancel func()
}

func New(in io.Reader, out io.Writer) *Asker {
	return &Asker{in: bufio.NewReader(in), out: out}
}

// Say prints a line of context for the next question to the same writer.
func (a *Asker) Say(format string, args ...any) {
	fmt.Fprintf(a.out, format, args...)
}

// line prints q and returns the trimmed answer; ok is false at EOF with no input.
func (a *Asker) line(q string) (string, bool) {
	fmt.Fprint(a.out, q)
	s, cancelled, err := a.read()
	if cancelled {
		return "", false
	}
	if err != nil && s == "" {
		fmt.Fprintln(a.out)
		return "", false
	}
	return strings.TrimSpace(s), true
}

// read reads one line. With Done set it reads in a goroutine and gives up when
// Done closes, after calling OnCancel. A read that outlives a cancel is left to
// the process exit.
func (a *Asker) read() (s string, cancelled bool, err error) {
	if a.Done == nil {
		s, err = a.in.ReadString('\n')
		return s, false, err
	}
	type result struct {
		s   string
		err error
	}
	select {
	case <-a.Done:
		return "", a.cancel(), nil
	default:
	}
	ch := make(chan result, 1)
	go func() {
		s, err := a.in.ReadString('\n')
		ch <- result{s, err}
	}()
	select {
	case r := <-ch:
		return r.s, false, r.err
	case <-a.Done:
		return "", a.cancel(), nil
	}
}

func (a *Asker) cancel() bool {
	if a.OnCancel != nil {
		a.OnCancel()
	}
	return true
}

func (a *Asker) Confirm(q string, def bool) bool {
	hint := "[y/N] "
	if def {
		hint = "[Y/n] "
	}
	for {
		s, ok := a.line(q + " " + hint)
		if !ok || s == "" {
			return def
		}
		switch strings.ToLower(s) {
		case "y", "yes":
			return true
		case "n", "no":
			return false
		}
	}
}

func (a *Asker) Choose(q string, def string, opts ...string) string {
	// Like bash's choose(): the default spelled out, the others as first letters.
	menu := make([]string, len(opts))
	for i, o := range opts {
		if o == def {
			menu[i] = o
		} else {
			menu[i] = o[:1]
		}
	}
	hint := "[" + strings.Join(menu, "/") + "] "
	for {
		s, ok := a.line(q + " " + hint)
		if !ok || s == "" {
			return def
		}
		s = strings.ToLower(s)
		for _, o := range opts {
			if s == strings.ToLower(o) || (len(s) == 1 && s[0] == strings.ToLower(o)[0]) {
				return o
			}
		}
	}
}

func (a *Asker) String(q string, required bool) string {
	for {
		s, ok := a.line(q)
		if !ok {
			return ""
		}
		if s != "" || !required {
			return s
		}
	}
}
