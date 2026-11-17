package tui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/morrieinmaas/athome/internal/steps"
)

type (
	eventMsg steps.Event
	logMsg   struct{}
	doneMsg  struct {
		rep steps.Report
		err error
	}
)

type row struct {
	name, state string
	err         error
}

// logBuf collects what env.Log would get; the view reads it while Execute writes.
type logBuf struct {
	mu  sync.Mutex
	buf strings.Builder
	p   *tea.Program
}

func (l *logBuf) Write(b []byte) (int, error) {
	l.mu.Lock()
	l.buf.Write(b)
	l.mu.Unlock()
	if l.p != nil {
		l.p.Send(logMsg{})
	}
	return len(b), nil
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

type progressModel struct {
	rows     []row
	spin     spinner.Model
	log      *logBuf
	showLog  bool
	started  chan struct{}
	finished bool
	canceled bool
	cancel   context.CancelFunc
	rep      steps.Report
	err      error
}

func (m *progressModel) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, func() tea.Msg {
		close(m.started)
		return nil
	})
}

func (m *progressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case eventMsg:
		for i := range m.rows {
			if m.rows[i].name == msg.Name {
				m.rows[i].state, m.rows[i].err = msg.State, msg.Err
			}
		}
	case doneMsg:
		m.finished, m.rep, m.err = true, msg.rep, msg.err
		if msg.err == nil || m.canceled {
			return m, tea.Quit
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			m.canceled = true
			m.cancel()
			return m, tea.Quit
		}
		if m.finished {
			return m, tea.Quit
		}
		if msg.String() == "l" {
			m.showLog = !m.showLog
		}
	}
	return m, nil
}

func (m *progressModel) View() string {
	var b strings.Builder
	for _, r := range m.rows {
		switch r.state {
		case "check", "apply":
			fmt.Fprintf(&b, "%s %s\n", m.spin.View(), r.name)
		case "done":
			fmt.Fprintf(&b, "✓ %s\n", r.name)
		case "skip":
			fmt.Fprintf(&b, "- %s (skipped)\n", r.name)
		case "fail":
			fmt.Fprintf(&b, "✗ %s: %v\n", r.name, r.err)
		default:
			fmt.Fprintf(&b, "  %s\n", r.name)
		}
	}
	if m.showLog {
		b.WriteString("\n--- run log ---\n" + m.log.String())
	}
	switch {
	case m.finished && m.err != nil:
		b.WriteString("\npress any key to exit\n")
	case !m.finished:
		b.WriteString("\nl: toggle log   ctrl-c: cancel\n")
	}
	return b.String()
}

// RunProgress runs list through steps.Execute in a goroutine while showing one
// line per step. wire, if set, is called once with an around func that suspends
// the screen for fn (sudo, interactive commands, prompts). On failure the view
// stays until a key press, then the Execute error is returned; Ctrl-C cancels
// the run and returns an error wrapping context.Canceled.
func RunProgress(ctx context.Context, in io.Reader, out io.Writer, env *steps.Env, list []steps.Step, wire func(around func(func()))) (steps.Report, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	lb := &logBuf{}
	m := &progressModel{
		spin:    spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(lipgloss.NewStyle())),
		log:     lb,
		started: make(chan struct{}),
		cancel:  cancel,
	}
	for _, s := range list {
		m.rows = append(m.rows, row{name: s.Name})
	}

	p := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(out))
	lb.p = p

	origLog, origEvents := env.Log, env.Events
	defer func() { env.Log, env.Events = origLog, origEvents }()
	if origLog != nil {
		env.Log = io.MultiWriter(origLog, lb)
	} else {
		env.Log = lb
	}
	env.Events = func(e steps.Event) {
		if origEvents != nil {
			origEvents(e)
		}
		p.Send(eventMsg(e))
	}
	if wire != nil {
		wire(func(fn func()) {
			_ = p.ReleaseTerminal()
			defer func() { _ = p.RestoreTerminal() }()
			fn()
		})
	}

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		select {
		case <-m.started:
		case <-ctx.Done():
		}
		rep, err := steps.Execute(ctx, env, list)
		p.Send(doneMsg{rep, err})
	}()

	_, runErr := p.Run()
	cancel()
	<-finished
	if runErr != nil {
		return m.rep, runErr
	}
	if m.canceled {
		return m.rep, fmt.Errorf("run canceled: %w", context.Canceled)
	}
	return m.rep, m.err
}
