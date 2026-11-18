package machines

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/morrieinmaas/athome/internal/keys"
	"github.com/morrieinmaas/athome/internal/prompt"
	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
)

// Deps is what the machine commands need from the process around them.
type Deps struct {
	Run     run.Runner
	Home    string
	Host    string        // laptop short hostname
	Version string        // running athome version
	Out     io.Writer     // progress and notes
	Ask     *prompt.Asker // nil: never prompt
	Backup  func(ctx context.Context) error
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
	return Refresh(d.Home, filepath.Base(keys.LaptopKey(d.Home, d.Host)))
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
	hostProbeScript = `printf "%s\n" "$HOME"; uname -n`
	readPubScript   = `cat "$HOME/.ssh/$1_ed25519.pub"`
	boxKeyScript    = prepareSSH + `k="$HOME/.ssh/$1_ed25519"
[ -f "$k" ] || ssh-keygen -q -t ed25519 -N '' -C "athome@$2" -f "$k" || exit 1
[ -f "$k.pub" ] || ssh-keygen -y -f "$k" > "$k.pub" || exit 1
cat "$k.pub"`
	readAuthorizedScript = `cat "$HOME/.ssh/authorized_keys"`
	ghStatusScript       = `gh auth status`
	ghLoginScript        = `gh auth login --hostname github.com --git-protocol ssh --skip-ssh-key --web`
)

func never(context.Context, *steps.Env) (bool, error) { return false, nil }

// AddSteps are the eight steps of machine add, in order. m is the machine
// being added (its pending entry when resuming); the steps fill in Home and Hostname.
func AddSteps(d Deps, m *Machine) []steps.Step {
	r := d.remote()
	gh := d.gh(*m) // its account follows m once machine-validate has resolved it
	box := func() *Box { return NewBox(r, *m) }
	savePending := func() error {
		if err := SavePending(d.Home, *m); err != nil {
			return err
		}
		return d.refresh()
	}
	return []steps.Step{
		{Name: "machine-validate", Check: never, Apply: func(ctx context.Context, e *steps.Env) error {
			if err := m.Validate(); err != nil {
				return err
			}
			if err := CheckLocalScope(d.Home, m.Scope); err != nil {
				return err
			}
			if err := CheckInclude(d.Home); err != nil {
				return err
			}
			if _, err := d.Run.LookPath("rbw"); err != nil {
				return keys.ErrNoRbw
			}
			if res, err := d.Run.Run(ctx, run.Cmd{Name: "rbw", Args: []string{"unlocked"}}); err != nil || res.Code != 0 {
				return keys.ErrLocked
			}
			// Pin the account now: a later gh auth switch must not move rm or a
			// rotation to another account.
			if m.GitHubAccount == "" {
				acct, err := gh.login(ctx)
				if err != nil {
					return fmt.Errorf("resolve the GitHub account (log in with gh auth login): %w", err)
				}
				m.GitHubAccount = acct
				if err := m.Validate(); err != nil {
					return err
				}
			}
			gh.account = m.GitHubAccount
			return nil
		}},
		{Name: "machine-host-key", Check: func(ctx context.Context, e *steps.Env) (bool, error) {
			if m.Home == "" || m.Hostname == "" {
				return false, nil
			}
			return d.hostKeyPinned(ctx, m.Name)
		}, Apply: func(ctx context.Context, e *steps.Env) error {
			if err := savePending(); err != nil {
				return err
			}
			res, err := d.Run.Run(ctx, run.Cmd{Name: "ssh", Args: r.sshArgs(
				[]string{"-o", "StrictHostKeyChecking=accept-new", "-o", "BatchMode=yes"}, m.Name, hostProbeScript, nil)})
			if err := okOrErr("read home and hostname", m.Name, res, err); err != nil {
				return err
			}
			lines := strings.Split(strings.TrimSpace(res.Stdout), "\n")
			if len(lines) < 2 {
				return fmt.Errorf("%s answered %q, want its home and hostname", m.Name, strings.TrimSpace(res.Stdout))
			}
			m.Home = strings.TrimSpace(lines[0])
			m.Hostname, _, _ = strings.Cut(strings.TrimSpace(lines[1]), ".")
			if err := m.Validate(); err != nil {
				return err
			}
			if ok, err := d.hostKeyPinned(ctx, m.Name); err != nil || !ok {
				return fmt.Errorf("no host key was pinned for %s in %s", m.Name, KnownHosts(d.Home))
			}
			if res, err := d.Run.Run(ctx, run.Cmd{Name: "ssh-keygen", Args: []string{"-l", "-F", m.Name, "-f", KnownHosts(d.Home)}}); err == nil {
				d.say("pinned host key (trust on first use) for %s: %s", m.Name, strings.TrimSpace(res.Stdout))
			}
			return savePending()
		}},
		{Name: "machine-box-key", Check: func(ctx context.Context, e *steps.Env) (bool, error) {
			res, err := r.Exec(ctx, m.Name, readPubScript, "", m.Hostname)
			if err != nil {
				return false, err
			}
			if err := reachErr(m.Name, res); err != nil {
				return false, err
			}
			if res.Code != 0 {
				return false, nil
			}
			auth, sign, err := gh.registered(ctx, blobOf(strings.TrimSpace(res.Stdout)))
			return auth && sign, err
		}, Apply: func(ctx context.Context, e *steps.Env) error {
			res, err := r.Exec(ctx, m.Name, boxKeyScript, "", m.Hostname, m.Name)
			if err := okOrErr("create the box key", m.Name, res, err); err != nil {
				return err
			}
			pub := strings.TrimSpace(res.Stdout)
			if err := checkPub(pub); err != nil {
				return fmt.Errorf("box key of %s: %w", m.Name, err)
			}
			if err := gh.register(ctx, m.Name, pub); err != nil {
				return err
			}
			return box().CheckGitHub(ctx, false)
		}},
		{Name: "machine-break-glass", Check: func(ctx context.Context, e *steps.Env) (bool, error) {
			pub, err := keys.BreakGlassPub(ctx, d.Run, m.Scope)
			if errors.Is(err, keys.ErrNoItem) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			res, err := r.Exec(ctx, m.Name, readAuthorizedScript, "")
			if err != nil {
				return false, err
			}
			if err := reachErr(m.Name, res); err != nil {
				return false, err
			}
			return res.Code == 0 && authorizedHas(res.Stdout, blobOf(pub)), nil
		}, Apply: func(ctx context.Context, e *steps.Env) error {
			pub, err := keys.BreakGlassPub(ctx, d.Run, m.Scope)
			if errors.Is(err, keys.ErrNoItem) {
				d.say("no break-glass key for %s yet; creating it", m.Scope)
				pub, err = keys.NewBreakGlass(ctx, d.Run, m.Scope)
			}
			if err != nil {
				return err
			}
			return box().Authorize(ctx, pub)
		}},
		{Name: "machine-bootstrap", Check: func(ctx context.Context, e *steps.Env) (bool, error) {
			return bootstrapDone(ctx, d, *m)
		}, Apply: func(ctx context.Context, e *steps.Env) error {
			return bootstrapBox(ctx, d, *m, true, d.outOrDiscard())
		}},
		{Name: "machine-gh-login", Check: func(ctx context.Context, e *steps.Env) (bool, error) {
			res, err := r.Exec(ctx, m.Name, ghStatusScript, "")
			if err != nil {
				return false, err
			}
			if err := reachErr(m.Name, res); err != nil {
				return false, err
			}
			return res.Code == 0, nil
		}, Apply: func(ctx context.Context, e *steps.Env) error {
			res, err := r.Interactive(ctx, m.Name, ghLoginScript)
			if err != nil {
				return fmt.Errorf("gh auth login on %s: %w", m.Name, err)
			}
			if res.Code != 0 {
				return fmt.Errorf("gh auth login on %s exited %d", m.Name, res.Code)
			}
			return nil
		}},
		{Name: "machine-grofe", Check: func(ctx context.Context, e *steps.Env) (bool, error) {
			has, ok, err := d.grofeHas(ctx, m.Name)
			if !ok && err == nil {
				d.say("grofe not installed: run grofe machine add %s %s once it is", m.Name, m.Name)
				return true, nil
			}
			return has, err
		}, Apply: func(ctx context.Context, e *steps.Env) error {
			g, _ := d.grofe()
			res, err := d.Run.Run(ctx, run.Cmd{Name: g, Args: []string{"machine", "add", m.Name, m.Name}})
			if err != nil {
				return err
			}
			if res.Code != 0 {
				return fmt.Errorf("grofe machine add %s %s failed: %s", m.Name, m.Name, strings.TrimSpace(res.Stderr))
			}
			return nil
		}},
		{Name: "machine-inventory", Check: never, Apply: func(ctx context.Context, e *steps.Env) error {
			if _, err := d.updateInventory(func(ms []Machine) ([]Machine, bool, error) {
				return append(ms, *m), true, nil
			}); err != nil {
				return err
			}
			if err := RemovePending(d.Home, m.Name); err != nil {
				return err
			}
			if err := d.refresh(); err != nil {
				return err
			}
			d.backup(ctx)
			return nil
		}},
	}
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
	if err := m.Validate(); err != nil {
		return err
	}
	ms, err := Load(d.Home)
	if err != nil {
		return err
	}
	if hasMachine(ms, m.Name) {
		return fmt.Errorf("machine %s already exists", m.Name)
	}
	p, err := LoadPending(d.Home, m.Name)
	if err != nil {
		return err
	}
	if p != nil {
		if !sameEntry(*p, m) {
			return fmt.Errorf("machine add %s is pending with other values; finish it with the same flags or run athome machine rm %s --force", m.Name, m.Name)
		}
		m = *p
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
	msg := fmt.Sprintf("athome machine add: %s: %v", failed.Name, failed.Err)
	if failed.Name == "machine-validate" {
		return errors.New(msg)
	}
	passed := append(rep.Applied, rep.Skipped...)
	if failed.Name == "machine-box-key" {
		// the keys may be on GitHub although the step failed after gh ssh-key add
		passed = append(passed, failed.Name)
	}
	var left []string
	for _, l := range leftBehind {
		if slices.Contains(passed, l.step) {
			left = append(left, strings.Replace(l.what, "%s", m.Name, 1))
		}
	}
	if len(left) == 0 {
		left = []string{"nothing"}
	}
	return fmt.Errorf("%s\nleft behind: %s\nrerun the same command to resume, or remove it with athome machine rm %s --force",
		msg, strings.Join(left, ", "), m.Name)
}
