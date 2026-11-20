package machines

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/morrieinmaas/athome/internal/config"
	"github.com/morrieinmaas/athome/internal/keys"
	"github.com/morrieinmaas/athome/internal/prompt"
	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
)

// Deps is what the machine commands need from the process around them.
type Deps struct {
	Run    run.Runner
	Home   string
	Host   string        // laptop short hostname
	Out    io.Writer     // progress and notes
	Ask    *prompt.Asker // nil: never prompt
	Backup func(ctx context.Context) error
}

// UsageError is a bad invocation: the cli exits 2 for it, 1 for any other error.
type UsageError string

func (e UsageError) Error() string { return string(e) }

func (d Deps) remote() Remote { return Remote{Run: d.Run, Home: d.Home, Host: d.Host} }

func (d Deps) say(format string, a ...any) {
	if d.Out != nil {
		fmt.Fprintf(d.Out, format+"\n", a...)
	}
}

// refresh rewrites the generated ssh config and the remote projects file.
func (d Deps) refresh() error {
	return RefreshTo(d.Home, filepath.Base(keys.LaptopKey(d.Home, d.Host)), d.outOrDiscard())
}

// boxConfig readies the generated ssh config for a command that reaches boxes:
// ~/.ssh/config must include it, and it is rewritten so every machine has its
// pinned Host block (a missing file would let ssh resolve the bare name outside it).
func (d Deps) boxConfig() error {
	if err := CheckInclude(d.Home); err != nil {
		return err
	}
	return d.refresh()
}

// updateInventory reloads the inventory, applies change to it, saves it and
// refreshes the generated ssh config and projects file. change reports whether
// it altered the list; when it did not, nothing is written. Applying only this
// command's change to a fresh load keeps a machine another command added
// meanwhile.
func (d Deps) updateInventory(change func(ms []Machine) ([]Machine, bool, error)) (bool, error) {
	ms, err := Load(d.Home)
	if err != nil {
		return false, err
	}
	ms, changed, err := change(ms)
	if err != nil || !changed {
		return false, err
	}
	if err := Save(d.Home, ms); err != nil {
		return false, err
	}
	return true, d.refresh()
}

// backup mirrors the inventory into the secrets vault; a failure is a warning.
func (d Deps) backup(ctx context.Context) {
	if d.Backup == nil {
		return
	}
	if err := d.Backup(ctx); err != nil {
		d.say("warning: the inventory changed but the backup failed (%v); run: athome secrets backup", err)
	}
}

// grofe is the command that reaches grofe: `grofe` on PATH, else the copy athome installs.
func (d Deps) grofe() (string, bool) {
	if _, err := d.Run.LookPath("grofe"); err == nil {
		return "grofe", true
	}
	if p := filepath.Join(d.Home, ".local", "lib", "grofe", "grofe"); fileExists(p) {
		return p, true
	}
	return "", false
}

// grofeHas reports whether grofe lists the machine; ok is false without grofe.
func (d Deps) grofeHas(ctx context.Context, name string) (has, ok bool, err error) {
	g, ok := d.grofe()
	if !ok {
		return false, false, nil
	}
	res, err := d.Run.Run(ctx, run.Cmd{Name: g, Args: []string{"machine", "ls"}})
	if err != nil {
		return false, true, err
	}
	if res.Code != 0 {
		return false, true, fmt.Errorf("grofe machine ls failed: %s", strings.TrimSpace(res.Stderr))
	}
	for _, l := range strings.Split(res.Stdout, "\n") {
		if f := strings.Fields(l); len(f) > 0 && f[0] == name {
			return true, true, nil
		}
	}
	return false, true, nil
}

func (d Deps) gh(m Machine) *ghClient { return &ghClient{r: d.Run, account: m.GitHubAccount} }

// reachErr turns ssh's own failure (exit 255) into an error; other codes are the script's.
func reachErr(name string, res run.Result) error {
	if res.Code == 255 {
		return fmt.Errorf("ssh to %s failed: %s", name, strings.TrimSpace(res.Stderr))
	}
	return nil
}

const (
	// hostProbeMark precedes the values, so a shell banner before it is ignored.
	hostProbeMark        = "athome-host-probe"
	hostProbeScript      = `echo ` + hostProbeMark + `; printf "%s\n" "$HOME"; uname -n`
	readPubScript        = `cat "$HOME/.ssh/$1_ed25519.pub"`
	readAuthorizedScript = `cat "$HOME/.ssh/authorized_keys"`
	ghStatusScript       = `gh auth status`
	ghLoginScript        = `gh auth login --hostname github.com --git-protocol ssh --skip-ssh-key --web`
)

func never(context.Context, *steps.Env) (bool, error) { return false, nil }

// addRun is what the add steps share. m follows the steps: machine-validate
// resolves the GitHub account, machine-host-key records home and hostname.
type addRun struct {
	d  Deps
	m  *Machine
	r  Remote
	gh *ghClient // its account follows m once machine-validate has resolved it
}

func (a *addRun) box() *Box { return NewBox(a.r, *a.m) }

func (a *addRun) savePending() error {
	if err := SavePending(a.d.Home, *a.m); err != nil {
		return err
	}
	return a.d.refresh()
}

// AddSteps are the eight steps of machine add, in order. m is the machine
// being added (its pending entry when resuming); the steps fill in Home and Hostname.
func AddSteps(d Deps, m *Machine) []steps.Step {
	a := &addRun{d: d, m: m, r: d.remote(), gh: d.gh(*m)}
	return []steps.Step{
		{Name: "machine-validate", Check: never, Apply: a.validateApply},
		{Name: "machine-host-key", Check: a.hostKeyCheck, Apply: a.hostKeyApply},
		{Name: "machine-box-key", Check: a.boxKeyCheck, Apply: a.boxKeyApply},
		{Name: "machine-break-glass", Check: a.breakGlassCheck, Apply: a.breakGlassApply},
		{Name: "machine-bootstrap", Check: a.bootstrapCheck, Apply: a.bootstrapApply},
		{Name: "machine-gh-login", Check: a.ghLoginCheck, Apply: a.ghLoginApply},
		{Name: "machine-grofe", Check: a.grofeCheck, Apply: a.grofeApply},
		{Name: "machine-inventory", Check: never, Apply: a.inventoryApply},
	}
}

func (a *addRun) validateApply(ctx context.Context, e *steps.Env) error {
	if err := a.m.Validate(); err != nil {
		return err
	}
	if err := CheckLocalScope(a.d.Home, a.m.Scope); err != nil {
		return err
	}
	if err := CheckInclude(a.d.Home); err != nil {
		return err
	}
	if _, err := a.d.Run.LookPath("rbw"); err != nil {
		return keys.ErrNoRbw
	}
	if res, err := a.d.Run.Run(ctx, run.Cmd{Name: "rbw", Args: []string{"unlocked"}}); err != nil || res.Code != 0 {
		return keys.ErrLocked
	}
	if a.m.GitHubAccount == "" {
		acct, err := a.gh.login(ctx)
		if err != nil {
			return fmt.Errorf("resolve the GitHub account (log in with gh auth login): %w", err)
		}
		a.m.GitHubAccount = acct
		if err := a.m.Validate(); err != nil {
			return err
		}
	}
	a.gh.account = a.m.GitHubAccount
	return nil
}

func (a *addRun) hostKeyCheck(ctx context.Context, e *steps.Env) (bool, error) {
	if a.m.Home == "" || a.m.Hostname == "" {
		return false, nil
	}
	return a.d.hostKeyPinned(ctx, a.m.Name)
}

func (a *addRun) hostKeyApply(ctx context.Context, e *steps.Env) error {
	m := a.m
	if err := a.savePending(); err != nil {
		return err
	}
	res, err := a.d.Run.Run(ctx, run.Cmd{Name: "ssh", Args: a.r.sshArgs(
		[]string{"-o", "StrictHostKeyChecking=accept-new", "-o", "BatchMode=yes"}, m.Name, hostProbeScript, nil)})
	if err := okOrErr("read home and hostname", m.Name, res, err); err != nil {
		return err
	}
	home, hostname, ok := parseHostProbe(res.Stdout)
	if !ok {
		return fmt.Errorf("%s answered %q, want its home and hostname", m.Name, strings.TrimSpace(res.Stdout))
	}
	m.Home = home
	m.Hostname, _, _ = strings.Cut(hostname, ".")
	if err := m.Validate(); err != nil {
		return err
	}
	if ok, err := a.d.hostKeyPinned(ctx, m.Name); err != nil || !ok {
		return fmt.Errorf("no host key was pinned for %s in %s", m.Name, KnownHosts(a.d.Home))
	}
	if res, err := a.d.Run.Run(ctx, run.Cmd{Name: "ssh-keygen", Args: []string{"-l", "-F", m.Name, "-f", KnownHosts(a.d.Home)}}); err == nil {
		a.d.say("pinned host key (trust on first use) for %s: %s", m.Name, strings.TrimSpace(res.Stdout))
	}
	return a.savePending()
}

// parseHostProbe reads home and hostname from the lines after hostProbeMark.
func parseHostProbe(out string) (home, hostname string, ok bool) {
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(out, "\r", "")), "\n")
	i := slices.Index(lines, hostProbeMark)
	if i < 0 || len(lines) < i+3 {
		return "", "", false
	}
	return strings.TrimSpace(lines[i+1]), strings.TrimSpace(lines[i+2]), true
}

func (a *addRun) breakGlassCheck(ctx context.Context, e *steps.Env) (bool, error) {
	pub, err := keys.BreakGlassPub(ctx, a.d.Run, a.m.Scope)
	if errors.Is(err, keys.ErrNoItem) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	res, err := a.r.Exec(ctx, a.m.Name, readAuthorizedScript, "")
	if err != nil {
		return false, err
	}
	if err := reachErr(a.m.Name, res); err != nil {
		return false, err
	}
	return res.Code == 0 && authorizedHas(res.Stdout, keys.PubBlob(pub)), nil
}

func (a *addRun) breakGlassApply(ctx context.Context, e *steps.Env) error {
	pub, err := keys.BreakGlassPub(ctx, a.d.Run, a.m.Scope)
	if errors.Is(err, keys.ErrNoItem) {
		a.d.say("no break-glass key for %s yet; creating it", a.m.Scope)
		pub, err = keys.NewBreakGlass(ctx, a.d.Run, a.m.Scope)
	}
	if err != nil {
		return err
	}
	return a.box().Authorize(ctx, pub)
}

func (a *addRun) bootstrapCheck(ctx context.Context, e *steps.Env) (bool, error) {
	return bootstrapDone(ctx, a.d, *a.m)
}

func (a *addRun) bootstrapApply(ctx context.Context, e *steps.Env) error {
	return bootstrapBox(ctx, a.d, *a.m, true, a.d.outOrDiscard())
}

func (a *addRun) grofeCheck(ctx context.Context, e *steps.Env) (bool, error) {
	has, ok, err := a.d.grofeHas(ctx, a.m.Name)
	if !ok && err == nil {
		a.d.say("grofe not installed: run grofe machine add %s %s once it is", a.m.Name, a.m.Name)
		return true, nil
	}
	return has, err
}

func (a *addRun) grofeApply(ctx context.Context, e *steps.Env) error {
	name := a.m.Name
	g, _ := a.d.grofe()
	res, err := a.d.Run.Run(ctx, run.Cmd{Name: g, Args: []string{"machine", "add", name, name}})
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("grofe machine add %s %s failed: %s", name, name, strings.TrimSpace(res.Stderr))
	}
	return nil
}

func (a *addRun) inventoryApply(ctx context.Context, e *steps.Env) error {
	if _, err := a.d.updateInventory(func(ms []Machine) ([]Machine, bool, error) {
		return append(ms, *a.m), true, nil
	}); err != nil {
		return err
	}
	if err := RemovePending(a.d.Home, a.m.Name); err != nil {
		return err
	}
	if err := a.d.refresh(); err != nil {
		return err
	}
	a.d.backup(ctx)
	return nil
}

func (d Deps) outOrDiscard() io.Writer {
	if d.Out == nil {
		return io.Discard
	}
	return d.Out
}

// authorizedHas reports whether an authorized_keys file lists blob, ignoring
// comment lines.
func authorizedHas(authorized, blob string) bool {
	for _, l := range strings.Split(authorized, "\n") {
		if strings.HasPrefix(strings.TrimLeft(l, " \t"), "#") {
			continue
		}
		if slices.Contains(strings.Fields(l), blob) {
			return true
		}
	}
	return false
}

// hostKeyPinned reports whether the pinned known_hosts holds a key for name.
func (d Deps) hostKeyPinned(ctx context.Context, name string) (bool, error) {
	res, err := d.Run.Run(ctx, run.Cmd{Name: "ssh-keygen", Args: []string{"-F", name, "-f", KnownHosts(d.Home)}})
	if err != nil {
		return false, err
	}
	return res.Code == 0, nil
}

// leftBehind names what a failed add already created, in step order.
var leftBehind = []struct{ step, what string }{
	{"machine-host-key", "pinned host key"},
	{"machine-box-key", "GitHub keys athome-%s[-sign]"},
	{"machine-break-glass", "break-glass authorization"},
	{"machine-bootstrap", "bootstrapped box"},
	{"machine-gh-login", "gh login on the box"},
	{"machine-grofe", "grofe machine"},
}

// sameEntry compares what the flags set; Home, Hostname and Repos are filled in
// by the add itself, and so is the GitHub account when the flag left it empty.
func sameEntry(a, b Machine) bool {
	a.Home, a.Hostname, a.Repos, a.Port = "", "", nil, a.port()
	b.Home, b.Hostname, b.Repos, b.Port = "", "", nil, b.port()
	return a.Name == b.Name && a.Scope == b.Scope && a.Target == b.Target && a.User == b.User && a.Jump == b.Jump &&
		a.Port == b.Port && a.Athome == b.Athome && a.ForwardAgent == b.ForwardAgent &&
		(b.GitHubAccount == "" || a.GitHubAccount == b.GitHubAccount)
}

// Add runs machine add's steps. A rerun with the same values resumes: every
// step whose check passes is skipped.
func Add(ctx context.Context, d Deps, m Machine) error {
	if m.Port == 0 {
		m.Port = defaultPort
	}
	if err := m.Validate(); err != nil {
		return err
	}
	if err := config.CheckNewMachineName(m.Name); err != nil {
		return fmt.Errorf("machine %s: name: %w", m.Name, err)
	}
	ms, err := Load(d.Home)
	if err != nil {
		return err
	}
	if m, err = resumeEntry(d, ms, m); err != nil {
		return err
	}
	if m.ForwardAgent {
		d.say("warning: root on %s can use your laptop key while you are attached", m.Name)
	}

	var failed steps.Event
	env := &steps.Env{Run: d.Run, Home: d.Home, Log: d.Out, Ask: d.Ask, Events: func(ev steps.Event) {
		if ev.State == "fail" {
			failed = ev
		}
	}}
	list := AddSteps(d, &m)
	rep, err := steps.Execute(ctx, env, list)
	if err == nil {
		d.say("✓ machine %s added (%s, %s)", m.Name, m.Scope, m.Target)
		return nil
	}
	if failed.Name == "" {
		return err
	}
	return addFailure(d, m.Name, failed, rep)
}

// resumeEntry is m, or its pending entry when a rerun with the same values
// resumes an unfinished add. A machine already in ms is refused.
func resumeEntry(d Deps, ms []Machine, m Machine) (Machine, error) {
	if hasMachine(ms, m.Name) {
		return m, fmt.Errorf("machine %s already exists", m.Name)
	}
	p, err := LoadPending(d.Home, m.Name)
	if err != nil {
		return m, err
	}
	if p == nil {
		return m, nil
	}
	if !sameEntry(*p, m) {
		return m, fmt.Errorf("machine add %s is pending with other values; finish it with the same flags or run athome machine rm %s --force", m.Name, m.Name)
	}
	return *p, nil
}

// addFailure reports the failed step and what the steps before it left behind.
func addFailure(d Deps, name string, failed steps.Event, rep steps.Report) error {
	msg := fmt.Sprintf("athome machine add: %s: %v", failed.Name, failed.Err)
	if failed.Name == "machine-validate" {
		return errors.New(msg)
	}
	passed := append(rep.Applied, rep.Skipped...)
	if failed.Name == "machine-box-key" {
		// the keys may be on GitHub although the step failed after gh ssh-key add
		passed = append(passed, failed.Name)
	}
	_, grofeInstalled := d.grofe()
	var left []string
	for _, l := range leftBehind {
		if l.step == "machine-grofe" && !grofeInstalled {
			continue // its check passed only because there is no grofe to add to
		}
		if slices.Contains(passed, l.step) {
			left = append(left, strings.Replace(l.what, "%s", name, 1))
		}
	}
	if len(left) == 0 {
		left = []string{"nothing"}
	}
	return fmt.Errorf("%s\nleft behind: %s\nrerun the same command to resume, or remove it with athome machine rm %s --force",
		msg, strings.Join(left, ", "), name)
}
