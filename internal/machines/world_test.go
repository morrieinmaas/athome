package machines

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
)

// world is a stateful fake of everything a machine command reaches: the
// boxes behind ssh, gh and its key listings, rbw, grofe and the laptop's
// ssh-keygen for known_hosts. It interprets a box script by identity, so a
// test sees what a real box would hold after each call.
type world struct {
	t    *testing.T
	home string

	mu    sync.Mutex
	log   []string // one entry per effect, in call order
	calls []run.Cmd
	have  map[string]bool
	fail  map[string]bool // op name -> fail while set
	boxes map[string]*fakeBox

	ghKeys  map[string][]fakeGHKey // endpoint -> keys
	ghEnv   []string               // GH_TOKEN values seen
	ghLogin string                 // the account gh api user answers with
	ghNext  int
	// ghForeign holds key blobs registered on some other account: adding one
	// answers "already", as GitHub does, and registers nothing here.
	ghForeign map[string]bool
	ghOwner   map[string]string // key blob -> the account that added it
	grofe     map[string]string // machine -> target
	grofeOK   bool

	rbwLocked bool
	rbwItems  map[string][]string
}

type fakeGHKey struct{ id, key, title string }

type fakeBox struct {
	down      bool
	hostname  string
	home      string
	pub       string
	keygens   int
	authkeys  string
	hasAuthK  bool
	bootstrap string
	version   string
	tag       string
	ghAuthed  bool
	mise      bool
	clones    map[string]bool
	envs      map[string]string // repo/file -> content
	hostKeyed bool
}

func newWorld(t *testing.T) (*world, Deps, *bytes.Buffer) {
	t.Helper()
	t.Setenv("HOSTNAME", "lap")
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte("Include athome/config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := &world{
		t: t, home: home,
		have:     map[string]bool{"rbw": true, "gh": true, "grofe": true},
		fail:     map[string]bool{},
		boxes:    map[string]*fakeBox{},
		ghKeys:   map[string][]fakeGHKey{},
		ghOwner:  map[string]string{},
		grofe:    map[string]string{},
		grofeOK:  true,
		rbwItems: map[string][]string{},
		ghLogin:  "octo",
	}
	out := &bytes.Buffer{}
	d := Deps{Run: w, Home: home, Host: "lap", Out: out, Backup: func(context.Context) error {
		w.add("backup")
		return nil
	}}
	return w, d, out
}

func (w *world) add(op string) { w.log = append(w.log, op) }

func (w *world) box(name string) *fakeBox {
	b := w.boxes[name]
	if b == nil {
		b = &fakeBox{hostname: "dev", home: "/home/dev", clones: map[string]bool{}, envs: map[string]string{}}
		w.boxes[name] = b
	}
	return b
}

// ops returns the log entries with the given prefix, in order.
func (w *world) ops(prefix string) []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []string
	for _, l := range w.log {
		if strings.HasPrefix(l, prefix) {
			out = append(out, l)
		}
	}
	return out
}

func (w *world) logCopy() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.log)
}

func (w *world) setFail(op string, on bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.fail[op] = on
}

func (w *world) LookPath(name string) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if name == "grofe" && !w.grofeOK {
		return "", exec.ErrNotFound
	}
	if w.have[name] {
		return "/fake/bin/" + name, nil
	}
	return "", exec.ErrNotFound
}

func unquote(s string) string {
	s = strings.TrimPrefix(strings.TrimSuffix(s, "'"), "'")
	return strings.ReplaceAll(s, `'\''`, "'")
}

func (w *world) Run(ctx context.Context, c run.Cmd) (run.Result, error) {
	if c.Name == "ssh-keygen" && !w.knownHostsOp(c) {
		return realKeygen(ctx, c) // the break-glass key generation; paths are test temp dirs
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, c)
	switch c.Name {
	case "ssh":
		return w.ssh(c), nil
	case "ssh-keygen":
		return w.keygenKnownHosts(c), nil
	case "gh":
		return w.gh(c), nil
	case "grofe":
		return w.grofeCmd(c), nil
	case "rbw":
		return w.rbw(c), nil
	}
	return run.Result{Code: 127, Stderr: "unexpected command " + c.Name}, nil
}

func (w *world) knownHostsOp(c run.Cmd) bool {
	return len(c.Args) > 0 && (c.Args[0] == "-F" || c.Args[0] == "-R" || c.Args[0] == "-l")
}

func realKeygen(ctx context.Context, c run.Cmd) (run.Result, error) {
	out, err := exec.CommandContext(ctx, "ssh-keygen", c.Args...).Output()
	if err != nil {
		code := 1
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		return run.Result{Code: code}, nil
	}
	return run.Result{Stdout: string(out)}, nil
}

// keygenKnownHosts models ssh-keygen -F / -l -F / -R on a plain known_hosts file.
func (w *world) keygenKnownHosts(c run.Cmd) run.Result {
	a := c.Args
	file := a[len(a)-1]
	name := a[len(a)-3]
	if a[0] == "-l" {
		name = a[2]
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return run.Result{Code: 1, Stderr: "no such file"}
	}
	var keep []string
	found := false
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.HasPrefix(l, name+" ") {
			found = true
			continue
		}
		keep = append(keep, l)
	}
	switch a[0] {
	case "-F":
		if !found {
			return run.Result{Code: 1}
		}
		return run.Result{Stdout: "# Host " + name + " found\n"}
	case "-l":
		if !found {
			return run.Result{Code: 1}
		}
		return run.Result{Stdout: "256 SHA256:FAKEFINGERPRINT " + name + " (ED25519)\n"}
	case "-R":
		w.add("keygen-R " + name)
		os.WriteFile(file, []byte(strings.Join(keep, "\n")+"\n"), 0o600)
		return run.Result{}
	}
	return run.Result{Code: 2}
}

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

func (w *world) gh(c run.Cmd) run.Result {
	for _, e := range c.Env {
		if v, ok := strings.CutPrefix(e, "GH_TOKEN="); ok {
			w.ghEnv = append(w.ghEnv, v)
		}
	}
	a := c.Args
	switch {
	case len(a) == 4 && a[0] == "api" && a[1] == "user" && a[2] == "--jq" && a[3] == ".login":
		w.add("gh-user")
		if w.ghLogin == "" {
			return run.Result{Code: 1, Stderr: "gh: not logged in"}
		}
		return run.Result{Stdout: w.ghLogin + "\n"}
	case len(a) > 1 && a[0] == "auth" && a[1] == "token":
		w.add("gh-token " + a[len(a)-1])
		return run.Result{Stdout: "tok-" + a[len(a)-1] + "\n"}
	case len(a) > 3 && a[0] == "api" && a[1] == "--paginate":
		var b strings.Builder
		for _, k := range w.ghKeys[a[2]] {
			fmt.Fprintf(&b, "%s %s %s\n", k.id, k.key, k.title)
		}
		return run.Result{Stdout: b.String()}
	case len(a) > 3 && a[0] == "ssh-key" && a[1] == "add":
		endpoint, op := "user/keys", "gh-add-auth"
		if slices.Contains(a, "signing") {
			endpoint, op = "user/ssh_signing_keys", "gh-add-sign"
		}
		if w.fail[op] {
			return run.Result{Code: 1, Stderr: "HTTP 404: Not Found (admin:public_key)"}
		}
		pub, _ := os.ReadFile(a[2])
		if w.ghForeign[strings.Fields(string(pub))[1]] {
			return run.Result{Code: 1, Stderr: "HTTP 422: key is already in use"}
		}
		w.ghNext++
		title := a[slices.Index(a, "--title")+1]
		w.ghKeys[endpoint] = append(w.ghKeys[endpoint], fakeGHKey{strconv.Itoa(w.ghNext), strings.Join(strings.Fields(string(pub))[:2], " "), title})
		w.ghOwner[strings.Fields(string(pub))[1]] = w.ghAccount(c)
		w.add(op + " " + title)
		return run.Result{}
	case len(a) == 4 && a[0] == "api" && a[1] == "-X" && a[2] == "DELETE":
		ep, id, _ := strings.Cut(strings.TrimPrefix(a[3], "user/"), "/")
		ep = "user/" + ep
		var keep []fakeGHKey
		for _, k := range w.ghKeys[ep] {
			if k.id == id {
				w.add("gh-delete " + ep + " " + k.title)
				continue
			}
			keep = append(keep, k)
		}
		w.ghKeys[ep] = keep
		return run.Result{}
	}
	return run.Result{Code: 2, Stderr: "fake gh: unexpected " + strings.Join(a, " ")}
}

// keyOwner is the account whose key this is; octo when a test seeded it.
func (w *world) keyOwner(key string) string {
	if a := w.ghOwner[strings.Fields(key)[len(strings.Fields(key))-1]]; a != "" {
		return a
	}
	return "octo"
}

// ghAccount is the account a gh call acts as: its GH_TOKEN's, else gh's own login.
func (w *world) ghAccount(c run.Cmd) string {
	for _, e := range c.Env {
		if v, ok := strings.CutPrefix(e, "GH_TOKEN=tok-"); ok {
			return v
		}
	}
	return w.ghLogin
}

func (w *world) grofeCmd(c run.Cmd) run.Result {
	a := c.Args
	switch {
	case len(a) == 2 && a[0] == "machine" && a[1] == "ls":
		var b strings.Builder
		for n, t := range w.grofe {
			fmt.Fprintf(&b, "%s\t%s\tmain\tssh\ttrue\n", n, t)
		}
		return run.Result{Stdout: b.String()}
	case len(a) == 4 && a[1] == "add":
		if w.fail["grofe-add"] {
			return run.Result{Code: 1, Stderr: "grofe-add failed"}
		}
		w.grofe[a[2]] = a[3]
		w.add("grofe machine add " + a[2] + " " + a[3])
		return run.Result{}
	case len(a) == 3 && a[1] == "rm":
		delete(w.grofe, a[2])
		w.add("grofe machine rm " + a[2])
		return run.Result{}
	}
	return run.Result{Code: 2, Stderr: "fake grofe: unexpected " + strings.Join(a, " ")}
}

// rbw models the parts of rbw break-glass storage uses; add and edit run the
// EDITOR script the way rbw does.
func (w *world) rbw(c run.Cmd) run.Result {
	a := c.Args
	switch a[0] {
	case "unlocked":
		if w.rbwLocked {
			return run.Result{Code: 1}
		}
		return run.Result{}
	case "list":
		var names []string
		for n, items := range w.rbwItems {
			for range items {
				names = append(names, n)
			}
		}
		slices.Sort(names)
		return run.Result{Stdout: strings.Join(names, "\n") + "\n"}
	case "get":
		if w.rbwLocked {
			return run.Result{Code: 1, Stderr: "locked"}
		}
		args := a[1:]
		field := ""
		if len(args) >= 2 && args[0] == "--field" {
			field, args = args[1], args[2:]
		}
		if len(args) != 1 || len(w.rbwItems[args[0]]) != 1 {
			return run.Result{Code: 1, Stderr: "no such entry"}
		}
		pw, notes, _ := strings.Cut(w.rbwItems[a[len(a)-1]][0], "\n")
		if field == "notes" {
			return run.Result{Stdout: notes}
		}
		return run.Result{Stdout: pw + "\n"}
	case "add", "edit":
		var editor string
		for _, e := range c.Env {
			if v, ok := strings.CutPrefix(e, "EDITOR="); ok {
				editor = v
			}
		}
		tmp := filepath.Join(w.t.TempDir(), "edit")
		if err := exec.Command("sh", editor, tmp).Run(); err != nil {
			return run.Result{Code: 1, Stderr: err.Error()}
		}
		b, err := os.ReadFile(tmp)
		if err != nil {
			return run.Result{Code: 1}
		}
		w.add("rbw-" + a[0] + " " + a[1])
		if a[0] == "add" {
			w.rbwItems[a[1]] = append(w.rbwItems[a[1]], string(b))
		} else if len(w.rbwItems[a[1]]) == 1 {
			w.rbwItems[a[1]][0] = string(b)
		}
		return run.Result{}
	}
	return run.Result{Code: 2}
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

func needKeygen(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("no ssh-keygen")
	}
}
