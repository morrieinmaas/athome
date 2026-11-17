package teardown_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/run/runtest"
	"github.com/morrieinmaas/athome/internal/teardown"
)

type rig struct {
	home string
	fake *runtest.Fake
	out  bytes.Buffer
	err  bytes.Buffer
}

func newRig(t *testing.T) *rig {
	t.Helper()
	return &rig{home: t.TempDir(), fake: runtest.NewFake()}
}

func (r *rig) run(osName, stdin string, args ...string) int {
	return teardown.Main(context.Background(), args, teardown.Env{
		Home: r.home, OS: osName, Hostname: "box", Run: r.fake,
		Stdin: strings.NewReader(stdin), Stdout: &r.out, Stderr: &r.err,
	})
}

func (r *rig) touch(t *testing.T, rel string) string {
	t.Helper()
	p := filepath.Join(r.home, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func contains(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("output lacks %q:\n%s", w, got)
		}
	}
}

func TestNoTierExits2WithUsage(t *testing.T) {
	r := newRig(t)
	if code := r.run("linux", ""); code != 2 {
		t.Fatalf("exit = %d", code)
	}
	contains(t, r.out.String()+r.err.String(), "pick a tier: --state | --dotfiles | --all")
}

func TestUnknownArgument(t *testing.T) {
	r := newRig(t)
	if code := r.run("linux", "", "--bogus"); code != 2 {
		t.Fatalf("exit = %d", code)
	}
	contains(t, r.err.String(), "athome teardown: unknown argument: --bogus (try --help)")
}

func TestHelpNamesTiers(t *testing.T) {
	for _, h := range []string{"-h", "--help"} {
		r := newRig(t)
		if code := r.run("linux", "", h); code != 0 {
			t.Fatalf("exit = %d", code)
		}
		contains(t, r.out.String(), "--state", "--dotfiles", "--all")
	}
}

func TestStateDryRunOnlyWouldLines(t *testing.T) {
	r := newRig(t)
	marker := r.touch(t, ".local/state/athome/github-manual-upload")
	r.fake.Have("chezmoi")
	if code := r.run("linux", "", "--state"); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	out := r.out.String()
	contains(t, out,
		"athome teardown: tier=state  mode=DRY-RUN (no changes)\n",
		"  would: chezmoi state reset --force\n",
		"  would: rm -rf "+r.home+"/.cache/chezmoi\n",
		"  would: rm -f "+r.home+"/.local/state/athome/github-manual-upload\n",
		"  would: rm -f "+r.home+"/.local/state/athome/ssh-pub-underivable\n",
		"  would: rm -f "+r.home+"/.local/state/athome/secrets-restore-attempted\n",
		"Dry run only, nothing changed. Re-run with --execute to apply.",
	)
	if strings.Contains(out, "\n  + ") {
		t.Errorf("dry run printed an executed line:\n%s", out)
	}
	if !exists(marker) {
		t.Error("dry run removed a file")
	}
	for _, c := range r.fake.Calls() {
		if c.Name == "chezmoi" && c.Args[0] == "state" {
			t.Errorf("dry run ran %v", c)
		}
	}
}

func TestLastTierWins(t *testing.T) {
	r := newRig(t)
	r.run("linux", "", "--all", "--state")
	contains(t, r.out.String(), "tier=state")
}

func TestExecuteAfterDryRunFlagLastWins(t *testing.T) {
	r := newRig(t)
	r.run("linux", "", "--state", "--execute", "--dry-run")
	contains(t, r.out.String(), "DRY-RUN")
}

func TestAllDryRunPlanDarwin(t *testing.T) {
	r := newRig(t)
	r.fake.Have("chezmoi", "nb", "rbw")
	r.fake.On("chezmoi", "managed", "--path-style=absolute", "--include=files").
		Return(run.Result{Stdout: r.touch(t, ".zshrc") + "\n" + r.home + "/gone\n"})
	r.fake.On("chezmoi", "source-path").Return(run.Result{Stdout: "/src/home\n"})
	r.fake.On("nb", "list").Return(run.Result{Stdout: "git 2.0\nfirefox 1.0 (cask)\nzed (cask)\n"})
	if code := r.run("darwin", "", "--all"); code != 0 {
		t.Fatalf("exit = %d: %s", code, r.err.String())
	}
	out := r.out.String()
	h := r.home
	ordered := []string{
		"== chezmoi persistent state + cache ==",
		"== chezmoi-managed files in $HOME ==",
		"  would: rm -f " + h + "/.zshrc\n",
		"(1 managed files)",
		"== generated chezmoi config ==",
		"  would: rm -rf " + h + "/.config/chezmoi\n",
		"kept: source repo /src/home",
		"== mise (tools + config) ==",
		"  would: rm -rf " + h + "/.local/share/mise " + h + "/.config/mise " + h + "/.local/bin/mise\n",
		"== chezmoi externals + app data (not 'managed files') ==",
		"== stale ~/.claude → agents symlinks ==",
		"== nanobrew casks (GUI apps in /Applications) ==",
		"  would: nb uninstall firefox\n",
		"  would: nb uninstall zed\n",
		"== nanobrew prefix (needs sudo) ==",
		"  would: sudo rm -rf /opt/nanobrew\n",
		"== rbw / Bitwarden local data ==",
		"  would: rbw stop-agent\n",
		"  would: rm -rf " + h + "/.config/rbw " + h + "/.local/share/rbw " + h + "/.cache/rbw\n",
		"== per-machine SSH key (backed up in Bitwarden) ==",
		"  would: rm -f " + h + "/.ssh/box_ed25519 " + h + "/.ssh/box_ed25519.pub\n",
		"== athome binary ==",
		"  would: rm -f " + h + "/.local/bin/athome\n",
		"== user data: LEFT ALONE (never removed) ==",
		"kept: ~/.secrets",
		"kept: ~/personal, ~/sidebiz, ~/work",
	}
	pos := 0
	for _, w := range ordered {
		i := strings.Index(out[pos:], w)
		if i < 0 {
			t.Fatalf("missing or out of order %q:\n%s", w, out)
		}
		pos += i + len(w)
	}
	if strings.Contains(out, "nb uninstall git") {
		t.Error("non-cask uninstalled")
	}
	for _, c := range r.fake.Calls() {
		if !(c.Name == "chezmoi" && (c.Args[0] == "managed" || c.Args[0] == "source-path")) && !(c.Name == "nb" && c.Args[0] == "list") {
			t.Errorf("dry run ran %s %v", c.Name, c.Args)
		}
	}
}

func TestAllLinuxPrintsPackageHint(t *testing.T) {
	r := newRig(t)
	r.fake.Have("pacman")
	r.run("linux", "", "--all")
	contains(t, r.out.String(), "pacman -Qqe", "left in place")
	if strings.Contains(r.out.String(), "nanobrew") {
		t.Error("linux plan mentions nanobrew")
	}
	r = newRig(t)
	r.fake.Have("dnf")
	r.run("linux", "", "--all")
	contains(t, r.out.String(), "dnf repoquery --userinstalled")
}

func TestChezmoiMissingSkips(t *testing.T) {
	r := newRig(t)
	if code := r.run("linux", "", "--dotfiles"); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	contains(t, r.out.String(), "  skip: chezmoi not found\n")
	if strings.Contains(r.out.String(), "would: chezmoi") {
		t.Error("planned a chezmoi call")
	}
}

func TestExecuteWithoutConfirmationAborts(t *testing.T) {
	for _, in := range []string{"\n", "", "dotfile\n", "all\n"} {
		r := newRig(t)
		f := r.touch(t, ".local/state/athome/ssh-pub-underivable")
		if code := r.run("linux", in, "--dotfiles", "--execute"); code != 2 {
			t.Fatalf("stdin %q: exit = %d", in, code)
		}
		contains(t, r.err.String(), "athome teardown: aborted, nothing was changed")
		contains(t, r.out.String(), `This will PERMANENTLY remove the above for tier "dotfiles".`, "Type dotfiles to proceed (anything else aborts): ")
		if !exists(f) {
			t.Errorf("stdin %q: file removed", in)
		}
	}
}

func TestExecuteRemovesAndKeepsUserData(t *testing.T) {
	r := newRig(t)
	r.fake.Have("chezmoi", "rbw")
	managed := r.touch(t, ".zshrc")
	marker := r.touch(t, ".local/state/athome/github-manual-upload")
	bin := r.touch(t, ".local/bin/athome")
	key := r.touch(t, ".ssh/box_ed25519")
	secret := r.touch(t, ".secrets/a.env")
	repo := r.touch(t, "personal/x/file")
	r.fake.On("chezmoi", "managed", "--path-style=absolute", "--include=files").Return(run.Result{Stdout: managed + "\n"})
	if code := r.run("linux", " all \n", "--all", "--execute"); code != 0 {
		t.Fatalf("exit = %d: %s", code, r.err.String())
	}
	for _, p := range []string{managed, marker, bin, key} {
		if exists(p) {
			t.Errorf("%s survived", p)
		}
	}
	for _, p := range []string{secret, repo} {
		if !exists(p) {
			t.Errorf("%s was removed", p)
		}
	}
	contains(t, r.out.String(), "  + rm -f "+managed+"\n", "  + chezmoi state reset --force\n",
		"✓ teardown (all) complete. Re-run ./scripts/bootstrap.sh for a fresh start.")
	var sawReset bool
	for _, c := range r.fake.Calls() {
		sawReset = sawReset || (c.Name == "chezmoi" && c.Args[0] == "state")
	}
	if !sawReset {
		t.Error("chezmoi state reset not run")
	}
}

func TestYesSkipsQuestion(t *testing.T) {
	r := newRig(t)
	f := r.touch(t, ".local/state/athome/ssh-pub-underivable")
	for _, flag := range []string{"--yes", "-y"} {
		if code := r.run("linux", "", "--state", "--run", flag); code != 0 {
			t.Fatalf("exit = %d", code)
		}
	}
	if exists(f) || strings.Contains(r.out.String(), "Type state") {
		t.Error("--yes did not skip the question or run")
	}
}

func TestClaudeLinkPruneOnlyAgentsLinks(t *testing.T) {
	r := newRig(t)
	claude := filepath.Join(r.home, ".claude")
	os.MkdirAll(claude, 0o755)
	os.Symlink(filepath.Join(r.home, ".config/agents/CLAUDE.md"), filepath.Join(claude, "CLAUDE.md"))
	os.Symlink("/elsewhere", filepath.Join(claude, "RTK.md"))
	r.touch(t, ".claude/settings.json")
	if code := r.run("linux", "", "--all", "--execute", "--yes"); code != 0 {
		t.Fatalf("exit = %d: %s", code, r.err.String())
	}
	if exists(filepath.Join(claude, "CLAUDE.md")) {
		t.Error("agents link survived")
	}
	if !exists(filepath.Join(claude, "RTK.md")) || !exists(filepath.Join(claude, "settings.json")) {
		t.Error("foreign link or real data removed")
	}
}

func TestFailingActionExits1(t *testing.T) {
	r := newRig(t)
	r.fake.Have("chezmoi")
	before := r.touch(t, ".local/state/athome/x")
	r.fake.On("chezmoi", "state", "reset", "--force").Return(run.Result{Code: 3, Stderr: "boom"})
	if code := r.run("linux", "", "--state", "--execute", "--yes"); code != 1 {
		t.Fatalf("exit = %d", code)
	}
	contains(t, r.err.String(), "athome teardown: chezmoi state reset --force failed")
	if !exists(before) || strings.Contains(r.out.String(), "complete") {
		t.Error("run continued or reported success")
	}
}

func TestRbwStopAgentFailureIgnored(t *testing.T) {
	r := newRig(t)
	r.fake.Have("rbw")
	r.fake.On("rbw", "stop-agent").Return(run.Result{Code: 1})
	if code := r.run("linux", "", "--all", "--execute", "--yes"); code != 0 {
		t.Fatalf("exit = %d: %s", code, r.err.String())
	}
}
