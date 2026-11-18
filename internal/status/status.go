// Package status reports whether athome and the tools it manages are current.
// Every external tool goes through run.Runner.
package status

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/selfupdate"
)

const (
	Current      = "current"
	NotCurrent   = "not current"
	NotInstalled = "not installed"
	Unknown      = "unknown"
)

type Component struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Installed string `json:"installed"`
	Wanted    string `json:"wanted"`
	Detail    string `json:"detail"`
}

type Report struct {
	Version    string         `json:"version"`
	Commit     string         `json:"commit"`
	Repo       string         `json:"repo"`
	Ref        string         `json:"ref"`
	Current    bool           `json:"current"`
	Components []Component    `json:"components"`
	Keys       KeysStatus     `json:"keys"`
	Machines   MachinesStatus `json:"machines"`
}

// Collect checks every component. Version and commit are the running binary's.
func Collect(ctx context.Context, r run.Runner, version, commit, home, repo string) Report {
	pins := readPins(repo)
	rep := Report{
		Version: version,
		Commit:  commit,
		Repo:    repo,
		Ref:     repoRef(ctx, r, repo),
		Components: []Component{
			athomeStatus(ctx, r, version, repo),
			dotfilesStatus(ctx, r),
			miseStatus(ctx, r, home),
			grofeStatus(ctx, r, home, pins["grofe"]),
			canopyStatus(ctx, r, home, pins["canopy"]),
		},
	}
	rep.Current = true
	for _, c := range rep.Components {
		rep.Current = rep.Current && c.Status == Current
	}
	return rep
}

// out runs a command and returns its trimmed stdout, false on a start error or non-zero exit.
func out(ctx context.Context, r run.Runner, name string, args ...string) (string, bool) {
	res, err := r.Run(ctx, run.Cmd{Name: name, Args: args})
	if err != nil || res.Code != 0 {
		return strings.TrimSpace(res.Stdout), false
	}
	return strings.TrimSpace(res.Stdout), true
}

func repoRef(ctx context.Context, r run.Runner, repo string) string {
	if s, ok := out(ctx, r, "git", "-C", repo, "symbolic-ref", "--short", "-q", "HEAD"); ok && s != "" {
		return s
	}
	if s, ok := out(ctx, r, "git", "-C", repo, "describe", "--tags", "--exact-match"); ok && s != "" {
		return s
	}
	if s, ok := out(ctx, r, "git", "-C", repo, "rev-parse", "--short", "HEAD"); ok && s != "" {
		return s
	}
	return "unknown"
}

func readPins(repo string) map[string]string {
	root := "home"
	if b, err := os.ReadFile(filepath.Join(repo, ".chezmoiroot")); err == nil && strings.TrimSpace(string(b)) != "" {
		root = strings.TrimSpace(string(b))
	}
	var f struct {
		Pins map[string]string `toml:"pins"`
	}
	if _, err := toml.DecodeFile(filepath.Join(repo, root, ".chezmoidata", "pins.toml"), &f); err != nil {
		return nil
	}
	return f.Pins
}

func athomeStatus(ctx context.Context, r run.Runner, version, repo string) Component {
	c := Component{Name: "athome", Installed: version}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	res, err := r.Run(ctx, run.Cmd{Name: "git", Args: []string{"-C", repo, "ls-remote", "--tags", "--refs", "origin"}})
	if err != nil || res.Code != 0 {
		c.Status, c.Detail = Unknown, "origin is unreachable"
		return c
	}
	best := selfupdate.Highest(selfupdate.RemoteTags(res.Stdout))
	if best == "" {
		c.Status, c.Detail = Unknown, "no release tags on origin"
		return c
	}
	c.Wanted = best
	switch _, ok := selfupdate.Parse(version); {
	case !ok:
		c.Status, c.Detail = Unknown, "a development build"
	case selfupdate.Compare(version, best) < 0:
		c.Status = NotCurrent
	default:
		c.Status = Current
	}
	return c
}

func dotfilesStatus(ctx context.Context, r run.Runner) Component {
	c := Component{Name: "dotfiles"}
	if _, err := r.LookPath("chezmoi"); err != nil {
		c.Status, c.Detail = NotInstalled, "chezmoi is missing"
		return c
	}
	s, ok := out(ctx, r, "chezmoi", "status", "--exclude=scripts")
	switch {
	case !ok:
		c.Status, c.Detail = Unknown, "chezmoi status failed"
	case s == "":
		c.Status = Current
	default:
		c.Status = NotCurrent
		c.Detail = fmt.Sprintf("%d file(s) differ", len(strings.Split(s, "\n")))
	}
	return c
}

func miseStatus(ctx context.Context, r run.Runner, home string) Component {
	c := Component{Name: "mise"}
	if _, err := r.LookPath("mise"); err != nil {
		c.Status, c.Detail = NotInstalled, "mise is missing"
		return c
	}
	res, err := r.Run(ctx, run.Cmd{Name: "mise", Args: []string{"ls", "--missing", "--json"}, Dir: home})
	if err != nil || res.Code != 0 {
		c.Status, c.Detail = Unknown, "mise ls failed"
		return c
	}
	n, err := MissingCount(res.Stdout)
	if err != nil {
		c.Status, c.Detail = Unknown, "mise printed unreadable output"
		return c
	}
	if n == 0 {
		c.Status = Current
		return c
	}
	c.Status, c.Detail = NotCurrent, fmt.Sprintf("%d tool(s) missing", n)
	return c
}

// MissingCount counts the tools in the output of `mise ls --missing --json`: empty
// output is none, and anything but a JSON object or array is an error.
func MissingCount(stdout string) (int, error) {
	stdout = strings.TrimSpace(stdout)
	if stdout == "" {
		return 0, nil
	}
	var v any
	if err := json.Unmarshal([]byte(stdout), &v); err != nil {
		return 0, fmt.Errorf("mise ls --missing --json: %w", err)
	}
	switch t := v.(type) {
	case map[string]any:
		return len(t), nil
	case []any:
		return len(t), nil
	}
	return 0, errors.New("mise ls --missing --json: expected a JSON object or array")
}

func grofeStatus(ctx context.Context, r run.Runner, home, pin string) Component {
	c := Component{Name: "grofe", Wanted: pin}
	bin := filepath.Join(home, ".local", "lib", "grofe", "grofe")
	if _, err := os.Stat(bin); err != nil {
		c.Status = NotInstalled
		return c
	}
	s, ok := out(ctx, r, bin, "-V")
	c.Installed = strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(s, "grofe")), "v")
	if _, isVer := selfupdate.Parse(c.Installed); !ok || !isVer {
		c.Status, c.Detail = Unknown, "version is not a release"
		return c
	}
	if pin == "" {
		c.Status, c.Detail = Unknown, "no pin in pins.toml"
		return c
	}
	if c.Installed == strings.TrimPrefix(pin, "v") {
		c.Status = Current
	} else {
		c.Status = NotCurrent
	}
	return c
}

func canopyStatus(ctx context.Context, r run.Runner, home, pin string) Component {
	c := Component{Name: "canopy", Wanted: pin}
	store := filepath.Join(home, ".local", "share", "canopy")
	if fi, err := os.Stat(store); err != nil || !fi.IsDir() {
		c.Status = NotInstalled
		return c
	}
	head, ok := out(ctx, r, "git", "-C", store, "rev-parse", "HEAD")
	if !ok {
		c.Status, c.Detail = Unknown, "canopy HEAD is unreadable"
		return c
	}
	c.Installed = head
	if pin == "" {
		c.Status, c.Detail = Unknown, "no pin in pins.toml"
		return c
	}
	want, ok := out(ctx, r, "git", "-C", store, "rev-parse", "--verify", pin+"^{commit}")
	switch {
	case !ok:
		c.Status, c.Detail = NotCurrent, "pin "+pin+" is not fetched"
	case want == head:
		c.Status = Current
	default:
		c.Status = NotCurrent
	}
	return c
}
