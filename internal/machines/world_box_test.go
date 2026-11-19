package machines

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/morrieinmaas/athome/internal/run"
)

func (w *world) ssh(c run.Cmd) run.Result {
	i := slices.Index(c.Args, "sh")
	if i < 1 || len(c.Args) < i+4 || c.Args[i+1] != "-c" {
		return run.Result{Code: 2, Stderr: "unexpected ssh " + strings.Join(c.Args, " ")}
	}
	name, script := c.Args[i-1], unquote(c.Args[i+2])
	var args []string
	for _, a := range c.Args[i+4:] {
		args = append(args, unquote(a))
	}
	b := w.box(name)
	if b.down {
		return run.Result{Code: 255, Stderr: "ssh: connect to host " + name + ": No route to host"}
	}
	interactive := slices.Contains(c.Args[:i-1], "-t")
	stdin := c.Stdin
	tag := ""
	if interactive {
		tag = "(interactive)"
	}
	failIf := func(op string) *run.Result {
		if w.fail[op] {
			return &run.Result{Code: 1, Stderr: op + " failed"}
		}
		return nil
	}
	switch script {
	case hostProbeScript:
		if r := failIf("probe"); r != nil {
			return *r
		}
		w.add("ssh-probe " + strings.Join(c.Args[:i-1], " "))
		kh := filepath.Join(w.home, ".ssh", "athome", "known_hosts")
		os.MkdirAll(filepath.Dir(kh), 0o700)
		f, _ := os.OpenFile(kh, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		fmt.Fprintf(f, "%s ssh-ed25519 AAAAFAKEHOSTKEY\n", name)
		f.Close()
		return run.Result{Stdout: "Welcome to the box\nlast login: yesterday\n" + hostProbeMark + "\n" + b.home + "\n" + b.hostname + ".example.org\n"}
	case readPubScript:
		if b.pub == "" {
			return run.Result{Code: 1, Stderr: "no such file"}
		}
		return run.Result{Stdout: b.pub + "\n"}
	case boxKeyScript:
		if r := failIf("keygen"); r != nil {
			return *r
		}
		if b.pub == "" {
			b.keygens++
			b.pub = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAABOXKEY" + strconv.Itoa(len(name)) + " athome@" + name
			w.add("box-keygen " + name)
		}
		return run.Result{Stdout: b.pub + "\n"}
	case checkGitHubScript:
		w.add("box-github-check " + name)
		if r := failIf("github-check"); r != nil {
			return *r
		}
		for _, k := range w.ghKeys["user/keys"] {
			if strings.Contains(b.pub, strings.Fields(k.key)[1]) {
				return run.Result{Stderr: "Hi " + w.keyOwner(k.key) + "! You've successfully authenticated, but GitHub does not provide shell access.", Code: 1}
			}
		}
		return run.Result{Stderr: "git@github.com: Permission denied (publickey).", Code: 255}
	case readAuthorizedScript:
		if !b.hasAuthK {
			return run.Result{Code: 1}
		}
		return run.Result{Stdout: b.authkeys}
	case authorizeScript:
		if r := failIf("authorize"); r != nil {
			return *r
		}
		w.add("box-authorize " + name)
		b.hasAuthK = true
		b.authkeys += stdin
		return run.Result{}
	case dropKeyScript:
		w.add("box-drop-key " + name + " " + args[0])
		var keep []string
		for _, l := range strings.Split(strings.TrimSpace(b.authkeys), "\n") {
			if !slices.Contains(strings.Fields(l), args[0]) {
				keep = append(keep, l)
			}
		}
		b.authkeys = strings.Join(keep, "\n") + "\n"
		return run.Result{}
	case pinnedVersionScript:
		if b.version == "" {
			return run.Result{Code: 127, Stderr: "not found"}
		}
		return run.Result{Stdout: "athome " + b.version + " (abc1234)\n"}
	case readBootstrapScript:
		if b.bootstrap == "" {
			return run.Result{Code: 1}
		}
		return run.Result{Stdout: b.bootstrap}
	case uploadScript:
		if r := failIf("upload"); r != nil {
			return *r
		}
		w.add("box-upload-bootstrap " + name)
		b.bootstrap = stdin
		return run.Result{}
	case checkoutScript:
		w.add("box-checkout " + name + " " + args[0])
		return run.Result{}
	case bootstrapScript, bootstrapScriptNI:
		if r := failIf("bootstrap"); r != nil {
			return *r
		}
		if slices.Contains(c.Args, "-T") != (script == bootstrapScriptNI) {
			return run.Result{Code: 2, Stderr: "-T and --non-interactive must go together"}
		}
		w.add("box-bootstrap " + name + " " + args[0] + tag + map[bool]string{true: " --non-interactive", false: ""}[script == bootstrapScriptNI])
		b.version = args[0]
		return run.Result{}
	case ghStatusScript:
		if b.ghAuthed {
			return run.Result{}
		}
		return run.Result{Code: 1, Stderr: "You are not logged into any GitHub hosts."}
	case ghLoginScript:
		if r := failIf("gh-login"); r != nil {
			return *r
		}
		w.add("box-gh-login " + name + tag)
		b.ghAuthed = true
		return run.Result{}
	case ghLogoutScript:
		w.add("box-gh-logout " + name)
		if !b.ghAuthed {
			return run.Result{Code: 1, Stderr: "You're not logged into any GitHub hosts."}
		}
		b.ghAuthed = false
		return run.Result{}
	case envCopyScript:
		w.add("box-env " + name + " " + args[0] + "/" + args[1])
		b.envs[args[0]+"/"+args[1]] = stdin
		return run.Result{}
	case mkWorktreesScript:
		w.add("box-mkdir " + args[0] + "/worktrees")
		return run.Result{}
	case cloneScript:
		if !b.clones[args[0]+"/"+args[1]] {
			w.add("box-clone " + args[2] + " " + args[0] + "/" + args[1])
			b.clones[args[0]+"/"+args[1]] = true
		}
		return run.Result{}
	case miseDetectScript:
		if b.mise {
			return run.Result{Stdout: "yes\n"}
		}
		return run.Result{}
	case miseInstallScript:
		w.add("box-mise-install " + args[0] + "/" + args[1])
		return run.Result{}
	}
	return run.Result{Code: 2, Stderr: "fake box does not know this script: " + script}
}

// sshArgs returns the recorded ssh invocations as joined args.
func (w *world) sshArgs() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []string
	for _, c := range w.calls {
		if c.Name == "ssh" {
			out = append(out, strings.Join(c.Args, " "))
		}
	}
	return out
}
