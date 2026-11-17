package tui

import (
	"context"
	"errors"
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
	runCtx   context.Context
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
		if msg.err != nil && (errors.Is(msg.err, context.Canceled) || (m.runCtx != nil && m.runCtx.Err() != nil)) {
			m.canceled = true // Ctrl-C while the terminal was released: a cancel, not a failure to acknowledge
		}
		if msg.err == nil || m.canceled {
			return m, tea.Quit
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case tea.KeyMsg:
		switch {
		case msg.String() == "l":
			m.showLog = !m.showLog
		case m.finished: // a failed run: any other key, Ctrl-C included, leaves with the step's error
			return m, tea.Quit
		case msg.Type == tea.KeyCtrlC:
			m.canceled = true
			m.cancel()
			return m, tea.Quit
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
		b.WriteString("\nl: toggle log   any other key: exit\n")
	case !m.finished:
		b.WriteString("\nl: toggle log   ctrl-c: cancel\n")
	}
	return b.String()
}

// progressRun ties a progressModel to steps.Execute: it runs the steps in a
// goroutine, feeds their events to the program and restores env afterwards.
type progressRun struct {
	m        *progressModel
	ctx      context.Context
	cancel   context.CancelFunc
	env      *steps.Env
	list     []steps.Step
	origLog  io.Writer
	origEv   func(steps.Event)
	release  func() error // the terminal hand-over around fn; nil means p's own
	restore  func() error
	finished chan struct{}
	rep      steps.Report // Execute's own result, set before finished closes
	err      error
}

func newProgressRun(ctx context.Context, env *steps.Env, list []steps.Step) *progressRun {
	ctx, cancel := context.WithCancel(ctx)
	r := &progressRun{
		ctx: ctx, cancel: cancel, env: env, list: list,
		origLog: env.Log, origEv: env.Events,
		finished: make(chan struct{}),
		m: &progressModel{
			spin:    spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(lipgloss.NewStyle())),
			log:     &logBuf{},
			started: make(chan struct{}),
			cancel:  cancel,
			runCtx:  ctx,
		},
	}
	for _, s := range list {
		r.m.rows = append(r.m.rows, row{name: s.Name})
	}
	return r
}

// start hooks env to p and runs the steps once the program has started.
func (r *progressRun) start(p *tea.Program, wire func(around func(func()))) {
	r.m.log.p = p
	env := r.env
	env.Log = r.m.log // the real stdout would garble the view; the view shows this log
	env.Events = func(e steps.Event) {
		if r.origEv != nil {
			r.origEv(e)
		}
		p.Send(eventMsg(e))
	}
	if wire != nil {
		release, restore := r.release, r.restore
		if release == nil {
			release = p.ReleaseTerminal
		}
		if restore == nil {
			restore = p.RestoreTerminal
		}
		wire(func(fn func()) {
			_ = release()
			defer func() { _ = restore() }()
			fn()
		})
	}
	go func() {
		defer close(r.finished)
		select {
		case <-r.m.started:
		case <-r.ctx.Done():
		}
		rep, err := steps.Execute(r.ctx, env, r.list)
		r.rep, r.err = rep, err
		p.Send(doneMsg{rep, err})
	}()
}

// result waits for the steps to stop, restores env and maps the end of the
// program to Execute's report and error. A program that quit before the done
// message (SIGTERM becomes a quit message) still returns Execute's own result.
func (r *progressRun) result(runErr error) (steps.Report, error) {
	r.cancel()
	<-r.finished
	r.env.Log, r.env.Events = r.origLog, r.origEv
	if r.origLog != nil { // the view is gone and the terminal is back: the run log goes where a plain run prints it
		io.WriteString(r.origLog, r.m.log.String())
	}
	switch {
	case errors.Is(runErr, tea.ErrInterrupted): // an external SIGINT, like Ctrl-C in the view
		return r.m.rep, fmt.Errorf("run canceled: %w", context.Canceled)
	case runErr != nil:
		return r.m.rep, runErr
	case r.m.canceled:
		return r.m.rep, fmt.Errorf("run canceled: %w", context.Canceled)
	case !r.m.finished:
		return r.rep, r.err
	}
	return r.m.rep, r.m.err
}

// RunProgress runs list through steps.Execute in a goroutine while showing one
// line per step. wire, if set, is called once with an around func that suspends
// the screen for fn (sudo, interactive commands, prompts). On failure the view
// stays until a key press, then the Execute error is returned; Ctrl-C cancels
// the run and returns an error wrapping context.Canceled.
func RunProgress(ctx context.Context, in io.Reader, out io.Writer, env *steps.Env, list []steps.Step, wire func(around func(func()))) (steps.Report, error) {
	r := newProgressRun(ctx, env, list)
	defer r.cancel()
	p := tea.NewProgram(r.m, tea.WithInput(in), tea.WithOutput(out))
	r.start(p, wire)
	_, runErr := p.Run()
	return r.result(runErr)
}
