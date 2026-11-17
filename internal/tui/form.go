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
// KeyExists is whether an ssh key exists for the machine; the key is named from
// chezmoi's hostname, never from the form's hostname answer.
type Seed struct {
	Values      map[string]string
	Locked      map[string]bool
	KeyExists   bool
	CheckURL    func(context.Context, string) error
	CheckHandle func(context.Context, string) error
}

// formState holds every answer as a string; huh fields bind to its pointers.
type formState struct {
	seed      Seed
	v         map[string]*string
	ssh       string // the ssh key choice: use|generate|reupload, or generate|skip
	sshHasKey bool   // whether a key exists; fixed by the seed
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
	"bitwardenUrl", "useBitwarden", "bwBackupSshKey", "uploadSshKeys", "restoreSecrets",
	"meshProvider", "nordvpnCountry", "netbirdManagementUrl",
	"includeAgents", "agentsRepo", "extraPackages",
	"slackDefaultWorkspace", "slackTeamId", "matterhornHost", "matterhornUser",
}

var formDefaults = map[string]string{
	"machineType": "personal", "meshProvider": "meshnet", "includeAgents": "true",
	"useBitwarden": "true", "bwBackupSshKey": "true", "uploadSshKeys": "true", "restoreSecrets": "true",
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
	s.sshHasKey = seed.KeyExists
	s.ssh = s.sshDefault()
	return s
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

// answers maps the state to the Data keys the steps read.
func (s *formState) answers() map[string]string {
	out := map[string]string{}
	for k, p := range s.v {
		out[k] = *p
	}
	if *s.v["useBitwarden"] == "false" { // the questions for these are hidden
		delete(out, "bitwardenUrl") // not asked, so not answered: a stored URL is kept
		out["restoreSecrets"] = "false"
	}
	if !s.generatesKey() || *s.v["useBitwarden"] == "false" { // the question is hidden
		delete(out, "bwBackupSshKey")
	}
	if s.sshHasKey {
		out["sshKeyAction"] = s.ssh
		if s.ssh == "reupload" { // the upload question is hidden: reupload means upload
			out["uploadSshKeys"] = "true"
		}
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
	// The hidden mesh fields are blank, locked or not.
	switch *s.v["meshProvider"] {
	case "none":
		out["nordvpnCountry"], out["netbirdManagementUrl"] = "", ""
	case "meshnet":
		out["netbirdManagementUrl"] = ""
	case "netbird":
		out["nordvpnCountry"] = ""
	}
	return out
}

// generatesKey reports whether the answered ssh choice makes a new key.
func (s *formState) generatesKey() bool {
	if s.sshHasKey {
		return s.ssh == "generate"
	}
	return s.ssh != "skip"
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

// pages lists the five editable pages, each built when it is reached, so the
// keys page sees the hostname answered on the first. The review page is separate.
func (s *formState) pages(ctx context.Context) []func() *huh.Form {
	return []func() *huh.Form{
		s.machinePage,
		func() *huh.Form { return s.identityPage(ctx) },
		func() *huh.Form { return s.keysPage(ctx) },
		s.networkPage,
		s.extrasPage,
	}
}

func (s *formState) machinePage() *huh.Form {
	return huh.NewForm(huh.NewGroup(
		s.choice("machineType", "Machine type", s.v["machineType"], "personal", "work"),
		s.input("hostname", "Hostname", "empty = this machine's name", validHostname),
	).Title("Machine"))
}

func (s *formState) identityPage(ctx context.Context) *huh.Form {
	return huh.NewForm(huh.NewGroup(
		s.input("githubHandle", "GitHub username", "e.g. octocat", validHandle(s.seed.CheckHandle, ctx, s.isSubmitting)),
		s.input("personalName", "Personal git name", "empty = your GitHub username", nil),
		s.input("workEmail", "Work git email", "empty = personal everywhere", validOptionalEmail),
		s.input("workName", "Work git name", "empty = personal name", nil),
		s.input("sidebizEmail", "Side-business git email", "empty = personal everywhere", validOptionalEmail),
		s.input("sidebizName", "Side-business git name", "empty = personal name", nil),
	).Title("Identity"))
}

func (s *formState) keysPage(ctx context.Context) *huh.Form {
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
		).Title("Keys and secrets"),
		huh.NewGroup(
			s.yesNo("uploadSshKeys", "Upload keys to GitHub?"),
		).Title("Keys and secrets").WithHideFunc(func() bool { return s.sshHasKey && s.ssh == "reupload" }),
		huh.NewGroup(
			s.input("bitwardenUrl", "Bitwarden server URL", "empty = US cloud; EU = https://vault.bitwarden.eu; or your Vaultwarden URL", validBitwardenURL(s.seed.CheckURL, ctx, s.isSubmitting)),
			s.yesNo("restoreSecrets", "Restore secrets from Bitwarden?"),
		).Title("Bitwarden").WithHideFunc(func() bool { return *s.v["useBitwarden"] == "false" }),
		huh.NewGroup(
			s.yesNo("bwBackupSshKey", "Store the ssh key in Bitwarden?"),
		).WithHideFunc(func() bool { return *s.v["useBitwarden"] == "false" || !s.generatesKey() }),
	)
}

func (s *formState) networkPage() *huh.Form {
	meshIs := func(m string) func() bool { return func() bool { return *s.v["meshProvider"] != m } }
	return huh.NewForm(
		huh.NewGroup(s.choice("meshProvider", "Mesh VPN", s.v["meshProvider"], "meshnet", "netbird", "none")).Title("Network"),
		huh.NewGroup(s.input("nordvpnCountry", "NordVPN country", "empty = NordVPN picks", nil)).WithHideFunc(meshIs("meshnet")),
		huh.NewGroup(s.input("netbirdManagementUrl", "NetBird management URL", "empty = NetBird Cloud", nil)).WithHideFunc(meshIs("netbird")),
	)
}

func (s *formState) extrasPage() *huh.Form {
	return huh.NewForm(huh.NewGroup(
		s.yesNo("includeAgents", "Set up agent skills?"),
		s.input("agentsRepo", "Agents repo (owner/repo)", "empty = none", nil),
		s.input("extraPackages", "Extra packages", "space-separated", nil),
		s.input("slackDefaultWorkspace", "Slack workspace slug", "empty = none", nil),
		s.input("slackTeamId", "Slack team id", "e.g. T0XXXXXXX", nil),
		s.input("matterhornHost", "Matterhorn host", "no https://, empty = none", nil),
		s.input("matterhornUser", "Matterhorn login", "empty = none", nil),
	).Title("Extras"))
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
		case "q", "ctrl+c":
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

// runProgram runs m until it quits; a cancelled ctx quits it too. It never
// uses tea.WithContext or tea.Interrupt: both take bubbletea's kill path, which
// closes the input reader without waiting for the read loop still using it (a
// data race, and the loop may outlive Run and touch the caller's reader).
func runProgram(ctx context.Context, m tea.Model, opts ...tea.ProgramOption) (tea.Model, error) {
	p := tea.NewProgram(m, opts...)
	defer context.AfterFunc(ctx, p.Quit)()
	return p.Run()
}

// RunForm shows the pages and returns the answers keyed as chezmoi's [data].
// A quit key or a cancelled context (SIGTERM) gives ErrQuit.
func RunForm(ctx context.Context, in io.Reader, out io.Writer, seed Seed) (map[string]string, error) {
	s := newFormState(seed)
	for {
		for _, page := range s.pages(ctx) {
			f := page().WithKeyMap(quitKeys)
			f.SubmitCmd, f.CancelCmd = tea.Quit, tea.Quit
			_, err := runProgram(ctx, f, tea.WithFilter(s.trackSubmit), tea.WithInput(in), tea.WithOutput(out))
			if f.State == huh.StateAborted || ctx.Err() != nil {
				return nil, ErrQuit
			}
			if err != nil {
				return nil, err
			}
		}
		rm := &reviewModel{text: s.reviewText()}
		_, err := runProgram(ctx, rm, tea.WithInput(in), tea.WithOutput(out))
		if ctx.Err() != nil {
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
