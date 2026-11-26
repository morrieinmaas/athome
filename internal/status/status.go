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

	// The command that brings a drifted component back to current.
	FixApply  = "chezmoi apply"
	FixUpdate = "athome update"

	offline = "origin is unreachable"
)

type Component struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Installed string `json:"installed"`
	Wanted    string `json:"wanted"`
	Pin       string `json:"pin,omitempty"`
	Detail    string `json:"detail"`
	Fix       string `json:"fix,omitempty"`
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
// A pins.toml it cannot read leaves grofe and canopy unknown.
func Collect(ctx context.Context, r run.Runner, version, commit, home, repo string) Report {
	pins, _ := readPins(repo)
	p := pinned(ctx, r, version, home, repo, pins)
	rep := Report{
		Version:    version,
		Commit:     commit,
		Repo:       repo,
		Ref:        repoRef(ctx, r, repo),
		Components: []Component{p[0], dotfilesStatus(ctx, r), miseStatus(ctx, r, home), p[1], p[2]},
	}
	rep.Current = true
	for _, c := range rep.Components {
		rep.Current = rep.Current && c.Status == Current
	}
	return rep
}

// Drift checks only the pinned tools (athome, grofe, canopy) and returns the ones
// that are behind, each with its Fix. dotfiles and mise are left out: a chezmoi
// apply would overwrite local edits. Anything it cannot judge is an error, so a
// broken check never reads as current; only an unreachable origin is not, since
// being offline is not drift.
func Drift(ctx context.Context, r run.Runner, version, home, repo string) ([]Component, error) {
	pins, err := readPins(repo)
	if err != nil {
		return nil, err
	}
	var drift []Component
	for _, c := range pinned(ctx, r, version, home, repo, pins) {
		if c.Status == Unknown && c.Detail != offline {
			return nil, fmt.Errorf("%s: %s", c.Name, c.Detail)
		}
		if c.Fix != "" {
			drift = append(drift, c)
		}
	}
	return drift, nil
}

// pinned checks athome, grofe and canopy against pins.toml.
func pinned(ctx context.Context, r run.Runner, version, home, repo string, pins map[string]string) []Component {
	return []Component{
		athomeStatus(ctx, r, version, repo, pins["athome"]),
		pinFix(grofeStatus(ctx, r, home, pins["grofe"])),
		pinFix(canopyStatus(ctx, r, home, pins["canopy"])),
	}
}

// pinFix marks a pinned tool that is behind its pin or missing: chezmoi apply installs it.
func pinFix(c Component) Component {
	if c.Status == NotCurrent || c.Status == NotInstalled {
		c.Fix = FixApply
	}
	return c
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

func readPins(repo string) (map[string]string, error) {
	root := "home"
	if b, err := os.ReadFile(filepath.Join(repo, ".chezmoiroot")); err == nil && strings.TrimSpace(string(b)) != "" {
		root = strings.TrimSpace(string(b))
	}
	var f struct {
		Pins map[string]string `toml:"pins"`
	}
	if _, err := toml.DecodeFile(filepath.Join(repo, root, ".chezmoidata", "pins.toml"), &f); err != nil {
		return nil, fmt.Errorf("pins.toml: %w", err)
	}
	return f.Pins, nil
}

// athomeStatus compares the running athome with the highest release tag on origin.
// Below pins.athome it is not current even offline: that is the floor chezmoi apply
// installs (run_14-install-athome), which athome update then moves beyond.
func athomeStatus(ctx context.Context, r run.Runner, version, repo, pin string) Component {
	c := tagStatus(ctx, r, version, repo)
	c.Pin = pin
	if c.Status == NotCurrent {
		c.Fix = FixUpdate
	}
	_, isRel := selfupdate.Parse(version)
	if _, pinRel := selfupdate.Parse(pin); isRel && pinRel && selfupdate.Compare(version, pin) < 0 {
		c.Status, c.Fix = NotCurrent, FixApply
		c.Detail = version + " (below pins.athome " + pin + "; chezmoi apply installs it)"
	}
	return c
}

func tagStatus(ctx context.Context, r run.Runner, version, repo string) Component {
	c := Component{Name: "athome", Installed: version}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	res, err := r.Run(ctx, run.Cmd{Name: "git", Args: []string{"-C", repo, "ls-remote", "--tags", "--refs", "origin"}})
	if err != nil || res.Code != 0 {
		c.Status, c.Detail = Unknown, offline
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
