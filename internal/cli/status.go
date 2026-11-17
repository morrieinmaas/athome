package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/morrieinmaas/athome/internal/run"
)

const (
	stCurrent      = "current"
	stNotCurrent   = "not current"
	stNotInstalled = "not installed"
	stUnknown      = "unknown"
)

type component struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Installed string `json:"installed"`
	Wanted    string `json:"wanted"`
	Detail    string `json:"detail"`
}

type statusReport struct {
	Version    string      `json:"version"`
	Commit     string      `json:"commit"`
	Repo       string      `json:"repo"`
	Ref        string      `json:"ref"`
	Current    bool        `json:"current"`
	Components []component `json:"components"`
}

func runStatus(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("athome status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "print the report as JSON")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Fprint(stdout, "Usage: athome status [--json]\n")
			return exitOK
		}
		fmt.Fprintf(stderr, "athome status: %v\n", err)
		return exitUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "athome status: unexpected argument %q\n", fs.Arg(0))
		return exitUsage
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "athome status: %v\n", err)
		return exitError
	}
	repo, err := findRepoRoot(home)
	if err != nil {
		fmt.Fprintf(stderr, "athome status: %v\n", err)
		return exitError
	}
	rep := collectStatus(context.Background(), newRunner(), home, repo)
	if *asJSON {
		b, _ := json.Marshal(rep)
		fmt.Fprintf(stdout, "%s\n", b)
		return exitOK
	}
	fmt.Fprintf(stdout, "athome %s (%s), repo %s at %s\n", rep.Version, rep.Commit, rep.Repo, rep.Ref)
	for _, c := range rep.Components {
		fmt.Fprintf(stdout, "%-9s %-14s %s\n", c.Name, c.Status, c.detailText())
	}
	return exitOK
}

func (c component) detailText() string {
	switch {
	case c.Detail != "":
		return c.Detail
	case c.Installed != "" && c.Wanted != "" && c.Installed != c.Wanted:
		return c.Installed + " (wanted " + c.Wanted + ")"
	}
	return c.Installed
}

func collectStatus(ctx context.Context, r run.Runner, home, repo string) statusReport {
	pins := readPins(repo)
	rep := statusReport{
		Version: Version,
		Commit:  Commit,
		Repo:    repo,
		Ref:     repoRef(ctx, r, repo),
		Components: []component{
			athomeStatus(ctx, r, repo),
			dotfilesStatus(ctx, r),
			miseStatus(ctx, r),
			grofeStatus(ctx, r, home, pins["grofe"]),
			canopyStatus(ctx, r, home, pins["canopy"]),
		},
	}
	rep.Current = true
	for _, c := range rep.Components {
		rep.Current = rep.Current && c.Status == stCurrent
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

// parseSemver reads vX.Y.Z or X.Y.Z; anything else is not a release.
func parseSemver(s string) ([3]int, bool) {
	var v [3]int
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func semverLess(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func athomeStatus(ctx context.Context, r run.Runner, repo string) component {
	c := component{Name: "athome", Installed: Version}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	res, err := r.Run(ctx, run.Cmd{Name: "git", Args: []string{"-C", repo, "ls-remote", "--tags", "--refs", "origin"}})
	if err != nil || res.Code != 0 {
		c.Status, c.Detail = stUnknown, "origin is unreachable"
		return c
	}
	var best [3]int
	var bestTag string
	for _, line := range strings.Split(res.Stdout, "\n") {
		_, ref, ok := strings.Cut(strings.TrimSpace(line), "\t")
		tag := strings.TrimPrefix(ref, "refs/tags/")
		if v, isVer := parseSemver(tag); ok && isVer && strings.HasPrefix(tag, "v") && (bestTag == "" || semverLess(best, v)) {
			best, bestTag = v, tag
		}
	}
	if bestTag == "" {
		c.Status, c.Detail = stUnknown, "no release tags on origin"
		return c
	}
	c.Wanted = bestTag
	have, ok := parseSemver(Version)
	switch {
	case !ok:
		c.Status, c.Detail = stUnknown, "a development build"
	case semverLess(have, best):
		c.Status = stNotCurrent
	default:
		c.Status = stCurrent
	}
	return c
}

func dotfilesStatus(ctx context.Context, r run.Runner) component {
	c := component{Name: "dotfiles"}
	if _, err := r.LookPath("chezmoi"); err != nil {
		c.Status, c.Detail = stNotInstalled, "chezmoi is missing"
		return c
	}
	s, ok := out(ctx, r, "chezmoi", "status", "--exclude=scripts")
	switch {
	case !ok:
		c.Status, c.Detail = stUnknown, "chezmoi status failed"
	case s == "":
		c.Status = stCurrent
	default:
		c.Status = stNotCurrent
		c.Detail = fmt.Sprintf("%d file(s) differ", len(strings.Split(s, "\n")))
	}
	return c
}

func miseStatus(ctx context.Context, r run.Runner) component {
	c := component{Name: "mise"}
	if _, err := r.LookPath("mise"); err != nil {
		c.Status, c.Detail = stNotInstalled, "mise is missing"
		return c
	}
	s, ok := out(ctx, r, "mise", "ls", "--missing", "--json")
	if !ok {
		c.Status, c.Detail = stUnknown, "mise ls failed"
		return c
	}
	n := 0
	if s != "" {
		var v any
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			c.Status, c.Detail = stUnknown, "mise printed unreadable output"
			return c
		}
		switch t := v.(type) {
		case map[string]any:
			n = len(t)
		case []any:
			n = len(t)
		}
	}
	if n == 0 {
		c.Status = stCurrent
		return c
	}
	c.Status, c.Detail = stNotCurrent, fmt.Sprintf("%d tool(s) missing", n)
	return c
}

func grofeStatus(ctx context.Context, r run.Runner, home, pin string) component {
	c := component{Name: "grofe", Wanted: pin}
	bin := filepath.Join(home, ".local", "lib", "grofe", "grofe")
	if _, err := os.Stat(bin); err != nil {
		c.Status = stNotInstalled
		return c
	}
	s, ok := out(ctx, r, bin, "-V")
	c.Installed = strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(s, "grofe")), "v")
	if _, isVer := parseSemver(c.Installed); !ok || !isVer {
		c.Status, c.Detail = stUnknown, "version is not a release"
		return c
	}
	if pin == "" {
		c.Status, c.Detail = stUnknown, "no pin in pins.toml"
		return c
	}
	if c.Installed == strings.TrimPrefix(pin, "v") {
		c.Status = stCurrent
	} else {
		c.Status = stNotCurrent
	}
	return c
}

func canopyStatus(ctx context.Context, r run.Runner, home, pin string) component {
	c := component{Name: "canopy", Wanted: pin}
	store := filepath.Join(home, ".local", "share", "canopy")
	if fi, err := os.Stat(store); err != nil || !fi.IsDir() {
		c.Status = stNotInstalled
		return c
	}
	head, ok := out(ctx, r, "git", "-C", store, "rev-parse", "HEAD")
	if !ok {
		c.Status, c.Detail = stUnknown, "canopy HEAD is unreadable"
		return c
	}
	c.Installed = head
	if pin == "" {
		c.Status, c.Detail = stUnknown, "no pin in pins.toml"
		return c
	}
	want, ok := out(ctx, r, "git", "-C", store, "rev-parse", "--verify", pin+"^{commit}")
	switch {
	case !ok:
		c.Status, c.Detail = stNotCurrent, "pin "+pin+" is not fetched"
	case want == head:
		c.Status = stCurrent
	default:
		c.Status = stNotCurrent
	}
	return c
}
