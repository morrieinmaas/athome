package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"

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
	height   int // terminal rows from the last tea.WindowSizeMsg; 0 until one arrives
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
		if msg.err == nil || m.canceled || errors.Is(msg.err, steps.ErrUpdateCancelled) { // a declined update is no failure: no held row
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.height = msg.Height
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

// stepLines renders one line per step.
func (m *progressModel) stepLines() []string {
	lines := make([]string, 0, len(m.rows))
	for _, r := range m.rows {
		switch r.state {
		case "check", "apply":
			lines = append(lines, fmt.Sprintf("%s %s", m.spin.View(), r.name))
		case "done":
			lines = append(lines, "✓ "+r.name)
		case "skip":
			lines = append(lines, "- "+r.name+" (skipped)")
		case "fail":
			lines = append(lines, fmt.Sprintf("✗ %s: %v", r.name, r.err))
		default:
			lines = append(lines, "  "+r.name)
		}
	}
	return lines
}

// focusRow is the row a window of steps is centred on: the running step, else
// the failed one, else the last step that has finished.
func (m *progressModel) focusRow() int {
	last := 0
	for i, r := range m.rows {
		switch r.state {
		case "check", "apply", "fail":
			return i
		case "done", "skip":
			last = i
		}
	}
	return last
}

// window picks h of the lines around focus.
func window(lines []string, focus, h int) []string {
	if h >= len(lines) {
		return lines
	}
	start := focus - h/2
	if start > len(lines)-h {
		start = len(lines) - h
	}
	if start < 0 {
		start = 0
	}
	return lines[start : start+h]
}

// View fits the terminal height once it is known: bubbletea drops the top lines
// of a taller view, which would hide the steps. The steps show a window around
// the running one, and the log gets the lines that are left, at least a third of
// the screen when the steps alone would fill it.
func (m *progressModel) View() string {
	lines := m.stepLines()
	var footer string
	switch {
	case m.finished && m.err != nil:
		footer = "\nl: toggle log   any other key: exit\n"
	case !m.finished:
		footer = "\nl: toggle log   ctrl-c: cancel\n"
	}
	var log []string
	if m.showLog {
		log = strings.Split(strings.TrimRight(m.log.String(), "\n"), "\n")
		if len(log) == 1 && log[0] == "" {
			log = nil
		}
	}
	if m.height > 0 {
		// The newline that ends the view counts as a line of its own.
		body := m.height - 1 - strings.Count(footer, "\n")
		if body < 1 {
			body = 1
		}
		if m.showLog {
			content := min(len(log), max(1, body/3-2))
			if free := body - 2 - len(lines); free > content {
				content = min(len(log), free)
			}
			log = log[len(log)-content:]
			lines = window(lines, m.focusRow(), max(1, body-2-content))
		} else {
			lines = window(lines, m.focusRow(), body)
		}
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	if m.showLog {
		b.WriteString("\n--- run log ---\n")
		for _, l := range log {
			b.WriteString(l + "\n")
		}
	}
	b.WriteString(footer)
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
	running  atomic.Bool // the program is alive: only then may the terminal be handed over
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
	r.running.Store(true)
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
			if !r.running.Load() { // the view quit: nothing to release, and restoring would revive a dead program
				fn()
				return
			}
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
	r.running.Store(false)
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
	r.running.Store(false)
	return r.result(runErr)
}
