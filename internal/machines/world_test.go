package machines

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

func needKeygen(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("no ssh-keygen")
	}
}

// hookRun runs before on every command, then hands it to the world. It lets a
// test change the inventory while a command is in the middle of its work.
type hookRun struct {
	*world
	before func(c run.Cmd)
}

func (h hookRun) Run(ctx context.Context, c run.Cmd) (run.Result, error) {
	h.before(c)
	return h.world.Run(ctx, c)
}
