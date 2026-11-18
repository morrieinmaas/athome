package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/morrieinmaas/athome/internal/machines"
)

// MachinesInfo is what the machines view shows: the `machine ls` text, the
// machine names to pick from, and the athome tag a new machine gets ("" asks).
type MachinesInfo struct {
	Listing string
	Names   []string
	Tag     string
	User    string // default user of the add form
}

// MachinesResult is the chosen action. Machine is set for "add"; Name for
// "sync" and "remove". Action is "back" when nothing is to run.
type MachinesResult struct {
	Action  string
	Name    string
	Machine machines.Machine
}

var machineActions = []string{"add", "sync", "sync all", "remove", "back"}

type machinesModel struct {
	info    MachinesInfo
	cursor  int
	stage   string // "", "pick" or "confirm"
	pick    int
	note    string
	pending string // the action waiting on a machine pick
	result  MachinesResult
}

func (m *machinesModel) Init() tea.Cmd { return nil }

func (m *machinesModel) finish(r MachinesResult) (tea.Model, tea.Cmd) {
	m.result = r
	return m, tea.Quit
}

func (m *machinesModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if k.String() == "ctrl+c" {
		return m.finish(MachinesResult{Action: "back"})
	}
	switch m.stage {
	case "pick":
		return m.updatePick(k.String())
	case "confirm":
		switch k.String() {
		case "y":
			return m.finish(MachinesResult{Action: "remove", Name: m.info.Names[m.pick]})
		default: // n, enter and everything else keeps the default: no
			m.stage = ""
		}
		return m, nil
	}
	m.note = ""
	switch k.String() {
	case "up", "k":
		m.cursor = (m.cursor + len(machineActions) - 1) % len(machineActions)
	case "down", "j", "tab":
		m.cursor = (m.cursor + 1) % len(machineActions)
	case "q", "esc":
		return m.finish(MachinesResult{Action: "back"})
	case "enter":
		return m.choose(machineActions[m.cursor])
	}
	return m, nil
}

func (m *machinesModel) choose(a string) (tea.Model, tea.Cmd) {
	switch a {
	case "add", "sync all", "back":
		return m.finish(MachinesResult{Action: a})
	}
	if len(m.info.Names) == 0 {
		m.note = "no machines"
		return m, nil
	}
	m.stage, m.pending, m.pick = "pick", a, 0
	return m, nil
}

func (m *machinesModel) updatePick(k string) (tea.Model, tea.Cmd) {
	n := len(m.info.Names)
	switch k {
	case "up", "k":
		m.pick = (m.pick + n - 1) % n
	case "down", "j", "tab":
		m.pick = (m.pick + 1) % n
	case "esc", "q":
		m.stage = ""
	case "enter":
		if m.pending == "remove" {
			m.stage = "confirm"
			return m, nil
		}
		return m.finish(MachinesResult{Action: m.pending, Name: m.info.Names[m.pick]})
	}
	return m, nil
}

func (m *machinesModel) View() string {
	var b strings.Builder
	b.WriteString("machines\n\n")
	b.WriteString(strings.TrimRight(m.info.Listing, "\n") + "\n\n")
	switch m.stage {
	case "pick":
		fmt.Fprintf(&b, "%s which machine?\n", m.pending)
		for i, n := range m.info.Names {
			cur := "  "
			if i == m.pick {
				cur = "> "
			}
			b.WriteString(cur + n + "\n")
		}
		return b.String()
	case "confirm":
		fmt.Fprintf(&b, "remove %s? This revokes its GitHub keys [y/N]\n", m.info.Names[m.pick])
		return b.String()
	}
	if m.note != "" {
		b.WriteString(m.note + "\n\n")
	}
	for i, a := range machineActions {
		cur := "  "
		if i == m.cursor {
			cur = "> "
		}
		b.WriteString(cur + a + "\n")
	}
	return b.String()
}

// machineFields builds a Machine around one field so the inventory's own
// validation judges it, and returns its message.
func machineField(set func(*machines.Machine)) error {
	m := machines.Machine{Name: "m", Scope: "work/x", Target: "h", User: "u", Port: 22, Athome: "v0.0.0"}
	set(&m)
	err := m.Validate()
	if err == nil {
		return nil
	}
	// "machine m: field: why" -> "field: why"
	msg := err.Error()
	if i := strings.Index(msg, ": "); i >= 0 && strings.HasPrefix(msg, "machine ") {
		msg = msg[i+2:]
	}
	return errors.New(msg)
}

func fieldValidator(set func(*machines.Machine, string)) func(string) error {
	return func(v string) error { return machineField(func(m *machines.Machine) { set(m, v) }) }
}

func validPort(v string) error {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return errors.New("port: must be a number")
	}
	return machineField(func(m *machines.Machine) { m.Port = n })
}

type addValues struct {
	name, scope, target, user, port, jump, account, tag string
	forward                                             string // "true" or "false"
}

func (a *addValues) machine() machines.Machine {
	port, _ := strconv.Atoi(strings.TrimSpace(a.port))
	return machines.Machine{
		Name: strings.TrimSpace(a.name), Scope: strings.TrimSpace(a.scope), Target: strings.TrimSpace(a.target),
		User: strings.TrimSpace(a.user), Port: port, Jump: strings.TrimSpace(a.jump),
		GitHubAccount: strings.TrimSpace(a.account), Athome: strings.TrimSpace(a.tag), ForwardAgent: a.forward == "true",
	}
}

func addForm(a *addValues, askTag bool) *huh.Form {
	trim := func(f func(*machines.Machine, string)) func(string) error {
		return fieldValidator(func(m *machines.Machine, v string) { f(m, strings.TrimSpace(v)) })
	}
	fields := []huh.Field{
		huh.NewInput().Title("Name").Description("lowercase, e.g. box1").Value(&a.name).
			Validate(trim(func(m *machines.Machine, v string) { m.Name = v })),
		huh.NewInput().Title("Scope").Description("work/<path>, personal/<path> or sidebiz/<path>").Value(&a.scope).
			Validate(trim(func(m *machines.Machine, v string) { m.Scope = v })),
		huh.NewInput().Title("Target").Description("host, mesh name or IPv4 address").Value(&a.target).
			Validate(trim(func(m *machines.Machine, v string) { m.Target = v })),
		huh.NewInput().Title("User").Value(&a.user).
			Validate(trim(func(m *machines.Machine, v string) { m.User = v })),
		huh.NewInput().Title("Port").Value(&a.port).Validate(validPort),
		huh.NewInput().Title("Jump host").Description("empty = none").Value(&a.jump).
			Validate(trim(func(m *machines.Machine, v string) { m.Jump = v })),
		huh.NewInput().Title("GitHub account").Description("empty = the box's default").Value(&a.account).
			Validate(trim(func(m *machines.Machine, v string) { m.GitHubAccount = v })),
		huh.NewSelect[string]().Title("Forward the ssh agent").Options(
			huh.NewOption("no", "false"), huh.NewOption("yes", "true")).Value(&a.forward),
	}
	if askTag {
		fields = append(fields, huh.NewInput().Title("athome tag").Description("this build has no release tag, e.g. v0.4.0").Value(&a.tag).
			Validate(trim(func(m *machines.Machine, v string) { m.Athome = v })))
	}
	return huh.NewForm(huh.NewGroup(fields...).Title("Add a machine")).WithKeyMap(quitKeys)
}

// RunMachines shows the machines view. "add" continues into the add form;
// the chosen action is returned for the caller to run.
func RunMachines(ctx context.Context, in io.Reader, out io.Writer, info MachinesInfo) (MachinesResult, error) {
	m := &machinesModel{info: info}
	if _, err := runProgram(ctx, m, tea.WithInput(in), tea.WithOutput(out)); err != nil {
		if errors.Is(err, tea.ErrInterrupted) {
			return MachinesResult{Action: "back"}, nil
		}
		return MachinesResult{}, err
	}
	res := m.result
	if res.Action == "" {
		res.Action = "back"
	}
	if res.Action != "add" {
		return res, nil
	}
	user := info.User
	if user == "" {
		user = os.Getenv("USER")
	}
	a := &addValues{user: user, port: "22", forward: "false", tag: info.Tag}
	f := addForm(a, info.Tag == "")
	f.SubmitCmd, f.CancelCmd = tea.Quit, tea.Quit
	_, err := runProgram(ctx, f, tea.WithInput(in), tea.WithOutput(out))
	if f.State == huh.StateAborted || ctx.Err() != nil || errors.Is(err, tea.ErrInterrupted) {
		return MachinesResult{Action: "back"}, nil
	}
	if err != nil {
		return MachinesResult{}, err
	}
	res.Machine = a.machine()
	return res, nil
}
