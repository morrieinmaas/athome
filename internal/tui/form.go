// Package tui holds the bubbletea screens: the init form, the progress view
// and the home screen.
package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// ErrQuit is returned when the user quits or aborts the form.
var ErrQuit = errors.New("form quit")

// Seed is what the form starts from. Values holds the prefilled answers keyed
// as chezmoi's [data]; Locked fields are shown but not editable and keep their value.
type Seed struct {
	Values      map[string]string
	Locked      map[string]bool
	KeyExists   bool
	CheckURL    func(context.Context, string) error
	CheckHandle func(context.Context, string) error
}

// formState holds every answer as a string; huh fields bind to its pointers.
type formState struct {
	seed Seed
	v    map[string]*string
	ssh  string // the ssh key choice: use|generate|reupload, or generate|skip
}

var formKeys = []string{
	"machineType", "hostname", "githubHandle", "githubId",
	"personalName", "workEmail", "workName", "sidebizEmail", "sidebizName",
	"bitwardenUrl", "useBitwarden", "uploadSshKeys", "restoreSecrets",
	"meshProvider", "nordvpnCountry", "netbirdManagementUrl",
	"includeAgents", "agentsRepo", "extraPackages",
	"slackDefaultWorkspace", "slackTeamId", "matterhornHost", "matterhornUser",
}

var formDefaults = map[string]string{
	"machineType": "personal", "meshProvider": "meshnet", "includeAgents": "true",
	"useBitwarden": "true", "uploadSshKeys": "true", "restoreSecrets": "true",
}

func newFormState(seed Seed) *formState {
	s := &formState{seed: seed, v: map[string]*string{}}
	for _, k := range formKeys {
		val := seed.Values[k]
		if val == "" {
			val = formDefaults[k]
		}
		s.v[k] = &val
	}
	switch {
	case seed.KeyExists:
		s.ssh = seed.Values["sshKeyAction"]
		if s.ssh == "" {
			s.ssh = "use"
		}
	case seed.Values["generateSshKey"] == "false":
		s.ssh = "skip"
	default:
		s.ssh = "generate"
	}
	return s
}

// answers maps the state to the Data keys the steps read.
func (s *formState) answers() map[string]string {
	out := map[string]string{}
	for k, p := range s.v {
		out[k] = *p
	}
	switch *s.v["meshProvider"] {
	case "none":
		out["nordvpnCountry"], out["netbirdManagementUrl"] = "", ""
	case "meshnet":
		out["netbirdManagementUrl"] = ""
	case "netbird":
		out["nordvpnCountry"] = ""
	}
	if s.seed.KeyExists {
		out["sshKeyAction"] = s.ssh
	} else if s.ssh == "skip" {
		out["generateSshKey"] = "false"
	} else {
		out["generateSshKey"] = "true"
	}
	// A seeded id belongs to the seeded handle; a changed handle must not keep it.
	if *s.v["githubHandle"] != s.seed.Values["githubHandle"] {
		out["githubId"] = ""
	}
	// Locked fields come back exactly as seeded.
	for k, locked := range s.seed.Locked {
		if !locked {
			continue
		}
		if val, ok := s.seed.Values[k]; ok {
			out[k] = val
		}
	}
	return out
}

func (s *formState) locked(k string) bool { return s.seed.Locked[k] }

func (s *formState) lockedKey(k string) bool {
	if k == "sshKey" {
		if s.seed.KeyExists {
			return s.locked("sshKeyAction")
		}
		return s.locked("generateSshKey")
	}
	return s.locked(k)
}

func (s *formState) input(k, title, desc string, validate func(string) error) huh.Field {
	if s.locked(k) {
		return lockedNote(title, s.seed.Values[k])
	}
	in := huh.NewInput().Title(title).Description(desc).Value(s.v[k])
	if validate != nil {
		in = in.Validate(validate)
	}
	return in
}

func (s *formState) choice(k, title string, ptr *string, opts ...string) huh.Field {
	if s.lockedKey(k) {
		return lockedNote(title, *ptr)
	}
	return huh.NewSelect[string]().Title(title).Options(huh.NewOptions(opts...)...).Value(ptr)
}

func lockedNote(title, value string) huh.Field {
	if value == "" {
		value = "(empty)"
	}
	return huh.NewNote().Title(title).Description(value + "  (set by flag or env, read-only)")
}

func (s *formState) yesNo(k, title string) huh.Field {
	if s.locked(k) {
		return lockedNote(title, *s.v[k])
	}
	return huh.NewSelect[string]().Title(title).Options(
		huh.NewOption("yes", "true"), huh.NewOption("no", "false")).Value(s.v[k])
}

// pages builds the five editable pages; the review page is separate.
func (s *formState) pages(ctx context.Context) []*huh.Form {
	sshOpts := []string{"generate", "skip"}
	sshTitle := "No ssh key yet: create one?"
	if s.seed.KeyExists {
		sshOpts = []string{"use", "generate", "reupload"}
		sshTitle = "An ssh key exists"
	}
	meshIs := func(m string) func() bool { return func() bool { return *s.v["meshProvider"] != m } }
	return []*huh.Form{
		huh.NewForm(huh.NewGroup(
			s.choice("machineType", "Machine type", s.v["machineType"], "personal", "work"),
			s.input("hostname", "Hostname", "", nil),
		).Title("Machine")),
		huh.NewForm(huh.NewGroup(
			s.input("githubHandle", "GitHub username", "e.g. octocat", validHandle(s.seed.CheckHandle, ctx)),
			s.input("personalName", "Personal git name", "empty = your GitHub username", nil),
			s.input("workEmail", "Work git email", "empty = personal everywhere", validOptionalEmail),
			s.input("workName", "Work git name", "empty = personal name", nil),
			s.input("sidebizEmail", "Side-business git email", "empty = personal everywhere", validOptionalEmail),
			s.input("sidebizName", "Side-business git name", "empty = personal name", nil),
		).Title("Identity")),
		huh.NewForm(huh.NewGroup(
			s.input("bitwardenUrl", "Bitwarden server URL", "empty = US cloud; EU = https://vault.bitwarden.eu; or your Vaultwarden URL", validBitwardenURL(s.seed.CheckURL, ctx)),
			s.yesNo("useBitwarden", "Store the ssh key in Bitwarden?"),
			s.choice("sshKey", sshTitle, &s.ssh, sshOpts...),
			s.yesNo("uploadSshKeys", "Upload keys to GitHub?"),
			s.yesNo("restoreSecrets", "Restore secrets from Bitwarden?"),
		).Title("Keys and secrets")),
		huh.NewForm(
			huh.NewGroup(s.choice("meshProvider", "Mesh VPN", s.v["meshProvider"], "meshnet", "netbird", "none")).Title("Network"),
			huh.NewGroup(s.input("nordvpnCountry", "NordVPN country", "empty = NordVPN picks", nil)).WithHideFunc(meshIs("meshnet")),
			huh.NewGroup(s.input("netbirdManagementUrl", "NetBird management URL", "empty = NetBird Cloud", nil)).WithHideFunc(meshIs("netbird")),
		),
		huh.NewForm(huh.NewGroup(
			s.yesNo("includeAgents", "Set up agent skills?"),
			s.input("agentsRepo", "Agents repo (owner/repo)", "empty = none", nil),
			s.input("extraPackages", "Extra packages", "space-separated", nil),
			s.input("slackDefaultWorkspace", "Slack workspace slug", "empty = none", nil),
			s.input("slackTeamId", "Slack team id", "e.g. T0XXXXXXX", nil),
			s.input("matterhornHost", "Matterhorn host", "no https://, empty = none", nil),
			s.input("matterhornUser", "Matterhorn login", "empty = none", nil),
		).Title("Extras")),
	}
}

var quitKeys = func() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("ctrl+c", "esc"))
	return km
}()

// reviewModel lists every answer: enter applies, b goes back, q or ctrl-c quits.
type reviewModel struct {
	text   string
	choice string // "apply", "back" or "quit"
}

func (m *reviewModel) Init() tea.Cmd { return nil }

func (m *reviewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "enter":
			m.choice = "apply"
		case "b":
			m.choice = "back"
		case "q", "ctrl+c", "esc":
			m.choice = "quit"
		default:
			return m, nil
		}
		return m, tea.Quit
	}
	return m, nil
}

func (m *reviewModel) View() string {
	if m.choice != "" {
		return ""
	}
	return m.text + "\nenter apply, b back to edit, q quit\n"
}

func (s *formState) reviewText() string {
	a := s.answers()
	var b strings.Builder
	b.WriteString("Review\n\n")
	for _, k := range append(append([]string{}, formKeys...), "sshKeyAction", "generateSshKey") {
		if v, ok := a[k]; ok && k != "githubId" {
			fmt.Fprintf(&b, "  %-22s %s\n", k, v)
		}
	}
	return b.String()
}

// RunForm shows the pages and returns the answers keyed as chezmoi's [data].
func RunForm(ctx context.Context, in io.Reader, out io.Writer, seed Seed) (map[string]string, error) {
	s := newFormState(seed)
	for {
		for _, f := range s.pages(ctx) {
			if err := f.WithInput(in).WithOutput(out).WithKeyMap(quitKeys).RunWithContext(ctx); err != nil {
				if errors.Is(err, huh.ErrUserAborted) || errors.Is(err, context.Canceled) {
					return nil, ErrQuit
				}
				return nil, err
			}
		}
		rm := &reviewModel{text: s.reviewText()}
		if _, err := tea.NewProgram(rm, tea.WithInput(in), tea.WithOutput(out), tea.WithContext(ctx)).Run(); err != nil {
			if errors.Is(err, tea.ErrProgramKilled) || errors.Is(err, context.Canceled) {
				return nil, ErrQuit
			}
			return nil, err
		}
		switch rm.choice {
		case "apply":
			return s.answers(), nil
		case "back":
		default:
			return nil, ErrQuit
		}
	}
}
