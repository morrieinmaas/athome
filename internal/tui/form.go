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
// KeyExists reports whether an ssh key exists for a hostname; it is asked with
// the hostname the form answers, and nil means no key.
type Seed struct {
	Values      map[string]string
	Locked      map[string]bool
	KeyExists   func(hostname string) bool
	CheckURL    func(context.Context, string) error
	CheckHandle func(context.Context, string) error
}

// formState holds every answer as a string; huh fields bind to its pointers.
type formState struct {
	seed Seed
	v    map[string]*string
	ssh  string // the ssh key choice: use|generate|reupload, or generate|skip
	// sshHasKey is the key existence ssh was last defaulted for; it is reset when
	// the answered hostname changes it.
	sshHasKey bool
	// submitting is true while the message being handled is an enter or tab key,
	// so a validator can tell a submit from huh's validation on blur.
	submitting bool
}

func (s *formState) isSubmitting() bool { return s.submitting }

// trackSubmit is a tea message filter that records whether the message is a submit key.
func (s *formState) trackSubmit(_ tea.Model, msg tea.Msg) tea.Msg {
	k, ok := msg.(tea.KeyMsg)
	s.submitting = ok && key.Matches(k, quitKeys.Input.Next, quitKeys.Input.Submit)
	return msg
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
	s.sshHasKey = s.keyExists()
	s.ssh = s.sshDefault()
	return s
}

// keyExists asks the seed about the hostname as currently answered.
func (s *formState) keyExists() bool {
	return s.seed.KeyExists != nil && s.seed.KeyExists(*s.v["hostname"])
}

func (s *formState) sshDefault() string {
	switch {
	case s.sshHasKey:
		if a := s.seed.Values["sshKeyAction"]; a != "" {
			return a
		}
		return "use"
	case s.seed.Values["generateSshKey"] == "false":
		return "skip"
	}
	return "generate"
}

// syncSSH resets the ssh choice when the answered hostname changed whether a key exists.
func (s *formState) syncSSH() {
	if has := s.keyExists(); has != s.sshHasKey {
		s.sshHasKey = has
		s.ssh = s.sshDefault()
	}
}

// answers maps the state to the Data keys the steps read.
func (s *formState) answers() map[string]string {
	s.syncSSH()
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
	if *s.v["useBitwarden"] == "false" { // the questions for these are hidden
		delete(out, "bitwardenUrl") // not asked, so not answered: a stored URL is kept
		out["restoreSecrets"] = "false"
	}
	if s.sshHasKey {
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
		if s.sshHasKey {
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

// pages builds the five editable pages, each when it is reached, so the keys
// page sees the hostname answered on the first. The review page is separate.
func (s *formState) pages(ctx context.Context) []func() *huh.Form {
	meshIs := func(m string) func() bool { return func() bool { return *s.v["meshProvider"] != m } }
	return []func() *huh.Form{
		func() *huh.Form {
			return huh.NewForm(huh.NewGroup(
				s.choice("machineType", "Machine type", s.v["machineType"], "personal", "work"),
				s.input("hostname", "Hostname", "empty = this machine's name", validHostname),
			).Title("Machine"))
		},
		func() *huh.Form {
			return huh.NewForm(huh.NewGroup(
				s.input("githubHandle", "GitHub username", "e.g. octocat", validHandle(s.seed.CheckHandle, ctx, s.isSubmitting)),
				s.input("personalName", "Personal git name", "empty = your GitHub username", nil),
				s.input("workEmail", "Work git email", "empty = personal everywhere", validOptionalEmail),
				s.input("workName", "Work git name", "empty = personal name", nil),
				s.input("sidebizEmail", "Side-business git email", "empty = personal everywhere", validOptionalEmail),
				s.input("sidebizName", "Side-business git name", "empty = personal name", nil),
			).Title("Identity"))
		},
		func() *huh.Form {
			s.syncSSH()
			sshOpts := []string{"generate", "skip"}
			sshTitle := "No ssh key yet: create one?"
			if s.sshHasKey {
				sshOpts = []string{"use", "generate", "reupload"}
				sshTitle = "An ssh key exists"
			}
			return huh.NewForm(
				huh.NewGroup(
					s.yesNo("useBitwarden", "Use Bitwarden (rbw) for secrets sync?"),
					s.choice("sshKey", sshTitle, &s.ssh, sshOpts...),
					s.yesNo("uploadSshKeys", "Upload keys to GitHub?"),
				).Title("Keys and secrets"),
				huh.NewGroup(
					s.input("bitwardenUrl", "Bitwarden server URL", "empty = US cloud; EU = https://vault.bitwarden.eu; or your Vaultwarden URL", validBitwardenURL(s.seed.CheckURL, ctx, s.isSubmitting)),
					s.yesNo("restoreSecrets", "Restore secrets from Bitwarden?"),
				).Title("Bitwarden").WithHideFunc(func() bool { return *s.v["useBitwarden"] == "false" }),
			)
		},
		func() *huh.Form {
			return huh.NewForm(
				huh.NewGroup(s.choice("meshProvider", "Mesh VPN", s.v["meshProvider"], "meshnet", "netbird", "none")).Title("Network"),
				huh.NewGroup(s.input("nordvpnCountry", "NordVPN country", "empty = NordVPN picks", nil)).WithHideFunc(meshIs("meshnet")),
				huh.NewGroup(s.input("netbirdManagementUrl", "NetBird management URL", "empty = NetBird Cloud", nil)).WithHideFunc(meshIs("netbird")),
			)
		},
		func() *huh.Form {
			return huh.NewForm(huh.NewGroup(
				s.yesNo("includeAgents", "Set up agent skills?"),
				s.input("agentsRepo", "Agents repo (owner/repo)", "empty = none", nil),
				s.input("extraPackages", "Extra packages", "space-separated", nil),
				s.input("slackDefaultWorkspace", "Slack workspace slug", "empty = none", nil),
				s.input("slackTeamId", "Slack team id", "e.g. T0XXXXXXX", nil),
				s.input("matterhornHost", "Matterhorn host", "no https://, empty = none", nil),
				s.input("matterhornUser", "Matterhorn login", "empty = none", nil),
			).Title("Extras"))
		},
	}
}

// Only ctrl+c quits: esc clears a select filter and must not end the form.
var quitKeys = func() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("ctrl+c"))
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
// A quit key, a cancelled context (SIGTERM) or huh's timeout all give ErrQuit.
func RunForm(ctx context.Context, in io.Reader, out io.Writer, seed Seed) (map[string]string, error) {
	s := newFormState(seed)
	for {
		for _, page := range s.pages(ctx) {
			err := page().WithProgramOptions(tea.WithFilter(s.trackSubmit)).WithInput(in).WithOutput(out).WithKeyMap(quitKeys).RunWithContext(ctx)
			if errors.Is(err, huh.ErrUserAborted) || errors.Is(err, huh.ErrTimeout) || errors.Is(err, context.Canceled) || ctx.Err() != nil {
				return nil, ErrQuit
			}
			if err != nil {
				return nil, err
			}
		}
		rm := &reviewModel{text: s.reviewText()}
		_, err := tea.NewProgram(rm, tea.WithInput(in), tea.WithOutput(out), tea.WithContext(ctx)).Run()
		if errors.Is(err, tea.ErrProgramKilled) || errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return nil, ErrQuit
		}
		if err != nil {
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
