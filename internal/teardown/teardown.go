// Package teardown undoes athome on this machine, in three cumulative tiers.
// It is a port of the old scripts/teardown.sh: a dry run by default, a typed
// confirmation before anything is deleted.
package teardown

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/morrieinmaas/athome/internal/run"
)

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

type Env struct {
	Home     string
	OS       string // runtime.GOOS
	Hostname string
	Run      run.Runner
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
	Color    bool
}

const helpText = `Usage: athome teardown (--state | --dotfiles | --all) [--execute] [--yes]

Cleanly undo athome on THIS machine, for a from-scratch retry. Tiers are
cumulative, each includes the ones above it:

  --state      chezmoi persistent state + cache only. Nothing is deleted from
               $HOME; this makes every run_once_ script run again and the
               config re-prompt.
  --dotfiles   --state + remove every chezmoi-managed file from $HOME and the
               generated chezmoi config. Leaves a bare shell. The source repo
               is kept so you can re-bootstrap.
  --all        --dotfiles + uninstall the tool layers: mise tools, nanobrew
               (/opt/nanobrew, needs sudo), rbw/Bitwarden local data, this
               machine's SSH key (backed up in Bitwarden) and ~/.local/bin/athome.

Dry run is the default: it prints what it would do and changes nothing.
  --execute, --run   do it; destructive tiers ask you to type the tier name
  --dry-run          preview only (the default)
  --yes, -y          skip the typed confirmation
  -h, --help         this text

Your data is never removed: ~/.secrets, ~/personal, ~/sidebiz, ~/work.
`

type item struct {
	section string // a section title, when set
	note    string // a plain line, printed in both modes
	show    string // the command as printed after "would:" or "+"
	do      func(ctx context.Context) error
}

type plan struct {
	items []item
	env   Env
}

func (p *plan) section(title string) { p.items = append(p.items, item{section: title}) }
func (p *plan) note(s string)        { p.items = append(p.items, item{note: s}) }
func (p *plan) add(show string, do func(context.Context) error) {
	p.items = append(p.items, item{show: show, do: do})
}

// rm plans a removal that runs in process and prints like the rm it replaces.
func (p *plan) rm(flag string, paths ...string) {
	p.add("rm "+flag+" "+strings.Join(paths, " "), func(context.Context) error {
		for _, path := range paths {
			var err error
			if flag == "-rf" {
				err = os.RemoveAll(path)
			} else {
				err = os.Remove(path)
				if os.IsNotExist(err) {
					err = nil
				}
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// cmd plans an external command; ignoreFail drops a non-zero exit.
func (p *plan) cmd(ignoreFail bool, c run.Cmd) {
	show := strings.Join(append([]string{c.Name}, c.Args...), " ")
	p.add(show, func(ctx context.Context) error {
		res, err := p.env.Run.Run(ctx, c)
		if err != nil {
			if ignoreFail {
				return nil
			}
			return err
		}
		if res.Code != 0 && !ignoreFail {
			return fmt.Errorf("exit %d: %s", res.Code, strings.TrimSpace(res.Stderr))
		}
		return nil
	})
}

type options struct {
	tier    string
	execute bool
	yes     bool
	help    bool
}

func Main(ctx context.Context, args []string, env Env) int {
	var o options
	for _, a := range args {
		switch a {
		case "--state":
			o.tier = "state"
		case "--dotfiles":
			o.tier = "dotfiles"
		case "--all":
			o.tier = "all"
		case "--execute", "--run":
			o.execute = true
		case "--dry-run":
			o.execute = false
		case "--yes", "-y":
			o.yes = true
		case "-h", "--help":
			fmt.Fprint(env.Stdout, helpText)
			return exitOK
		default:
			fmt.Fprintf(env.Stderr, "athome teardown: unknown argument: %s (try --help)\n", a)
			return exitUsage
		}
	}
	if o.tier == "" {
		fmt.Fprint(env.Stdout, helpText)
		fmt.Fprintf(env.Stderr, "\nathome teardown: pick a tier: --state | --dotfiles | --all\n")
		return exitUsage
	}

	c := newColors(env.Color)
	mode := "DRY-RUN (no changes)"
	if o.execute {
		mode = "EXECUTE"
	}
	fmt.Fprintf(env.Stdout, "%sathome teardown%s: tier=%s  mode=%s\n", c.ylw, c.rst, o.tier, mode)

	p := build(ctx, env, o.tier)
	p.print(env.Stdout, c)
	if !o.execute {
		fmt.Fprintf(env.Stdout, "\n%sDry run only, nothing changed.%s Re-run with --execute to apply.\n", c.dim, c.rst)
		return exitOK
	}
	if !o.yes {
		fmt.Fprintf(env.Stdout, "\n%sThis will PERMANENTLY remove the above for tier \"%s\".%s\n", c.red, o.tier, c.rst)
		fmt.Fprintf(env.Stdout, "Type %s%s%s to proceed (anything else aborts): ", c.red, o.tier, c.rst)
		line, _ := bufio.NewReader(env.Stdin).ReadString('\n')
		if strings.TrimSpace(line) != o.tier {
			fmt.Fprintln(env.Stdout)
			fmt.Fprintln(env.Stderr, "athome teardown: aborted, nothing was changed")
			return exitUsage
		}
	}
	if err := p.execute(ctx, env, c); err != nil {
		fmt.Fprintf(env.Stderr, "athome teardown: %v\n", err)
		return exitError
	}
	fmt.Fprintf(env.Stdout, "\n%s✓ teardown (%s) complete.%s Re-run ./scripts/bootstrap.sh for a fresh start.\n", c.grn, o.tier, c.rst)
	return exitOK
}

type colors struct{ red, grn, ylw, dim, rst string }

func newColors(on bool) colors {
	if !on {
		return colors{}
	}
	return colors{"\033[31m", "\033[32m", "\033[33m", "\033[2m", "\033[0m"}
}

// print shows the plan, each action as "would:".
func (p *plan) print(w io.Writer, c colors) {
	for _, it := range p.items {
		switch {
		case it.section != "":
			fmt.Fprintf(w, "\n%s== %s ==%s\n", c.ylw, it.section, c.rst)
		case it.note != "":
			fmt.Fprintf(w, "  %s%s%s\n", c.dim, it.note, c.rst)
		default:
			fmt.Fprintf(w, "  %swould:%s %s\n", c.dim, c.rst, it.show)
		}
	}
}

func (p *plan) execute(ctx context.Context, env Env, c colors) error {
	for _, it := range p.items {
		switch {
		case it.section != "":
			fmt.Fprintf(env.Stdout, "\n%s== %s ==%s\n", c.ylw, it.section, c.rst)
		case it.note != "":
			fmt.Fprintf(env.Stdout, "  %s%s%s\n", c.dim, it.note, c.rst)
		default:
			fmt.Fprintf(env.Stdout, "  %s+%s %s\n", c.grn, c.rst, it.show)
			if err := it.do(ctx); err != nil {
				return fmt.Errorf("%s failed: %w", it.show, err)
			}
		}
	}
	return nil
}

func build(ctx context.Context, env Env, tier string) *plan {
	p := &plan{env: env}
	p.state()
	if tier == "state" {
		return p
	}
	p.dotfiles(ctx)
	if tier == "dotfiles" {
		return p
	}
	p.all(ctx)
	return p
}

func (p *plan) hasChezmoi() bool {
	_, err := p.env.Run.LookPath("chezmoi")
	return err == nil
}

func (p *plan) path(rel ...string) string {
	return filepath.Join(append([]string{p.env.Home}, rel...)...)
}

func (p *plan) state() {
	p.section("chezmoi persistent state + cache")
	// --force skips chezmoi's own prompt, which a headless run cannot answer.
	if p.hasChezmoi() {
		p.cmd(false, run.Cmd{Name: "chezmoi", Args: []string{"state", "reset", "--force"}})
	} else {
		p.note("skip: chezmoi not found")
	}
	p.rm("-rf", p.path(".cache", "chezmoi"))
	// athome's markers; left behind, a rebootstrap would skip the matching step.
	for _, m := range []string{"github-manual-upload", "ssh-pub-underivable", "secrets-restore-attempted"} {
		p.rm("-f", p.path(".local", "state", "athome", m))
	}
}

func (p *plan) dotfiles(ctx context.Context) {
	section := "chezmoi-managed files in $HOME"
	p.section(section)
	src := ""
	if p.hasChezmoi() {
		count := 0
		res, _ := p.env.Run.Run(ctx, run.Cmd{Name: "chezmoi", Args: []string{"managed", "--path-style=absolute", "--include=files"}})
		for _, f := range strings.Split(res.Stdout, "\n") {
			if fi, err := os.Stat(f); f != "" && err == nil && fi.Mode().IsRegular() {
				p.rm("-f", f)
				count++
			}
		}
		p.note(fmt.Sprintf("(%d managed files)", count))
		sp, _ := p.env.Run.Run(ctx, run.Cmd{Name: "chezmoi", Args: []string{"source-path"}})
		src = strings.TrimSpace(sp.Stdout)
	} else {
		p.note("skip: chezmoi not found")
	}
	p.section("generated chezmoi config")
	p.rm("-rf", p.path(".config", "chezmoi"))
	if src == "" {
		src = "?"
	}
	p.note(fmt.Sprintf("kept: source repo %s (needed to re-bootstrap)", src))
}

func (p *plan) all(ctx context.Context) {
	p.section("mise (tools + config)")
	p.rm("-rf", p.path(".local", "share", "mise"), p.path(".config", "mise"), p.path(".local", "bin", "mise"))

	p.section("chezmoi externals + app data (not 'managed files')")
	// Git externals and app-generated data never show in `chezmoi managed`.
	// Not ~/.claude: that is the user's real Claude Code data.
	p.rm("-rf", p.path(".local", "share", "zinit"), p.path(".config", "tmux", "plugins"),
		p.path(".config", "agents"), p.path(".local", "share", "nvim"),
		p.path(".local", "state", "nvim"), p.path(".cache", "bat"))

	// Only symlinks under ~/.claude that point into ~/.config/agents go.
	p.section("stale ~/.claude → agents symlinks")
	for _, item := range []string{"CLAUDE.md", "AGENTS.md", "AGENTIC-SYSTEMS.md", "RTK.md", "skills", "agents"} {
		link := p.path(".claude", item)
		target, err := os.Readlink(link)
		if err == nil && strings.HasPrefix(target, p.path(".config", "agents")+string(filepath.Separator)) {
			p.rm("-f", link)
		}
	}

	if p.env.OS == "darwin" {
		p.nanobrew(ctx)
	} else {
		p.linuxNote()
	}

	p.section("rbw / Bitwarden local data")
	if _, err := p.env.Run.LookPath("rbw"); err == nil {
		p.cmd(true, run.Cmd{Name: "rbw", Args: []string{"stop-agent"}})
	}
	p.rm("-rf", p.path(".config", "rbw"), p.path(".local", "share", "rbw"), p.path(".cache", "rbw"))

	p.section("per-machine SSH key (backed up in Bitwarden)")
	p.rm("-f", p.path(".ssh", p.env.Hostname+"_ed25519"), p.path(".ssh", p.env.Hostname+"_ed25519.pub"))

	// Only the binary in ~/.local/bin; one elsewhere on PATH is left alone.
	p.section("athome binary")
	p.rm("-f", p.path(".local", "bin", "athome"))

	// USER DATA is never removed by any tier: ~/.secrets, ~/personal, ~/sidebiz, ~/work.
	p.section("user data: LEFT ALONE (never removed)")
	p.note("kept: ~/.secrets (decrypted .env files from the secrets vault)")
	p.note("kept: ~/personal, ~/sidebiz, ~/work (your repos + project files)")
}

func (p *plan) nanobrew(ctx context.Context) {
	p.section("nanobrew casks (GUI apps in /Applications)")
	// Casks first: removing the prefix would leave the .app bundles behind.
	if _, err := p.env.Run.LookPath("nb"); err == nil {
		res, _ := p.env.Run.Run(ctx, run.Cmd{Name: "nb", Args: []string{"list"}})
		for _, line := range strings.Split(res.Stdout, "\n") {
			if f := strings.Fields(line); len(f) > 0 && strings.Contains(line, "(cask)") {
				p.cmd(false, run.Cmd{Name: "nb", Args: []string{"uninstall", f[0]}})
			}
		}
	}
	p.section("nanobrew prefix (needs sudo)")
	p.cmd(false, run.Cmd{Name: "sudo", Args: []string{"rm", "-rf", "/opt/nanobrew"}, Interactive: true})
}

func (p *plan) linuxNote() {
	name, list, remove := "pacman/yay", "pacman -Qqe", "sudo pacman -Rns <pkg>"
	_, dnf := p.env.Run.LookPath("dnf")
	_, pacman := p.env.Run.LookPath("pacman")
	if dnf == nil && pacman != nil {
		name, list, remove = "dnf", "dnf repoquery --userinstalled", "sudo dnf remove <pkg>"
	}
	p.section(fmt.Sprintf("Linux system packages (%s): left in place", name))
	p.note(fmt.Sprintf("note: packages were installed system-wide via %s and are", name))
	p.note("NOT auto-removed (could break the OS). Remove by hand if needed:")
	p.note(fmt.Sprintf("  %s   # list explicitly-installed, then  %s", list, remove))
}
