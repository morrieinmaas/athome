package machines

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/morrieinmaas/athome/internal/keys"
	"github.com/morrieinmaas/athome/internal/run"
)

// The sshd drill runs Box, LoginCheck and keys.Rotate against a real sshd in a
// container (test/sshd). It needs ATHOME_SSHD_ADDR (host:port) and
// ATHOME_SSHD_KEY (the private key whose public half the container was started
// with); .github/workflows/sshd.yml sets both.

// drillRunner is the real exec runner with two changes. ssh calls are pinned to
// the temp HOME: ssh expands "~" from the passwd database, not $HOME, so the
// generated config's ~/.ssh paths would otherwise point at the real home. gh,
// rbw and chezmoi are fakes; ssh-keygen and ssh to the container are real.
type drillRunner struct {
	t    *testing.T
	home string
	host string
	real run.Runner

	mu    sync.Mutex
	items map[string]string // rbw item -> password + "\n" + notes
}

func (d *drillRunner) LookPath(name string) (string, error) {
	switch name {
	case "rbw":
		return "rbw", nil
	case "chezmoi", "gh":
		return "", errors.New("not available in the drill")
	}
	return d.real.LookPath(name)
}

func (d *drillRunner) Run(ctx context.Context, c run.Cmd) (run.Result, error) {
	switch c.Name {
	case "ssh":
		return d.ssh(ctx, c)
	case "gh":
		return run.Result{}, nil // adds succeed, the key lists are empty
	case "rbw":
		return d.rbw(c), nil
	}
	return d.real.Run(ctx, c)
}

func (d *drillRunner) ssh(ctx context.Context, c run.Cmd) (run.Result, error) {
	if slices.Contains(c.Args, "git@github.com") {
		// Never reach GitHub from a test.
		return run.Result{Stderr: "Hi drill! You've successfully authenticated, but GitHub does not provide shell access."}, nil
	}
	pre := []string{
		"-o", "UserKnownHostsFile=" + KnownHosts(d.home),
		"-o", "ControlMaster=no", "-o", "ControlPath=none",
		"-o", "ConnectTimeout=10",
	}
	if !slices.Contains(c.Args, "-F") {
		// Not a login check: the athome config with the laptop key.
		pre = append(pre, "-F", filepath.Join(d.home, ".ssh", "config"), "-i", keys.LaptopKey(d.home, d.host))
	}
	c.Args = append(pre, c.Args...)
	return d.real.Run(ctx, c)
}

func (d *drillRunner) rbw(c run.Cmd) run.Result {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch c.Args[0] {
	case "unlocked":
		return run.Result{}
	case "list":
		var names []string
		for n := range d.items {
			names = append(names, n)
		}
		slices.Sort(names)
		return run.Result{Stdout: strings.Join(names, "\n") + "\n"}
	case "remove":
		delete(d.items, c.Args[1])
		return run.Result{}
	case "get":
		args := c.Args[1:]
		field := ""
		if len(args) == 3 && args[0] == "--field" {
			field, args = args[1], args[2:]
		}
		item, ok := d.items[args[0]]
		if !ok {
			return run.Result{Code: 1, Stderr: "no such entry"}
		}
		pw, notes, _ := strings.Cut(item, "\n")
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
		tmp := filepath.Join(d.t.TempDir(), "edit")
		if err := exec.Command("sh", editor, tmp).Run(); err != nil {
			return run.Result{Code: 1, Stderr: err.Error()}
		}
		b, err := os.ReadFile(tmp)
		if err != nil {
			return run.Result{Code: 1, Stderr: err.Error()}
		}
		d.items[c.Args[1]] = string(b)
		return run.Result{}
	}
	return run.Result{Code: 2}
}

func (d *drillRunner) notes(item string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, notes, _ := strings.Cut(d.items[item], "\n")
	return strings.TrimRight(notes, "\n") + "\n"
}

func genKey(t *testing.T, path string) string {
	t.Helper()
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "drill", "-f", path).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v: %s", err, out)
	}
	b, err := os.ReadFile(path + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

// scanHostKey pins the container's ed25519 host key under the alias drill.
func scanHostKey(t *testing.T, home, host, port string) {
	t.Helper()
	var out []byte
	var err error
	for i := 0; i < 20; i++ {
		out, err = exec.Command("ssh-keyscan", "-t", "ed25519", "-p", port, host).Output()
		if err == nil && strings.Contains(string(out), "ssh-ed25519") {
			break
		}
		time.Sleep(time.Second)
	}
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Fields(l)
		if len(f) == 3 && f[1] == "ssh-ed25519" {
			if err := os.WriteFile(KnownHosts(home), []byte("drill "+f[1]+" "+f[2]+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("ssh-keyscan found no ed25519 host key at %s:%s: %v", host, port, err)
}

func TestSshdDrill(t *testing.T) {
	addr, keyPath := os.Getenv("ATHOME_SSHD_ADDR"), os.Getenv("ATHOME_SSHD_KEY")
	if addr == "" || keyPath == "" {
		t.Skip("set ATHOME_SSHD_ADDR and ATHOME_SSHD_KEY to run the sshd drill (see test/sshd)")
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	portN, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Nothing may read or write the real home, for athome or for ssh.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SSH_AUTH_SOCK", "")
	t.Setenv("HOSTNAME", "drillhost")
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}

	// The laptop key is the key the container started with.
	const laptopHost = "drillhost"
	laptop := keys.LaptopKey(home, laptopHost)
	priv, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(laptop, priv, 0o600); err != nil {
		t.Fatal(err)
	}
	pubOut, err := exec.Command("ssh-keygen", "-y", "-f", laptop).Output()
	if err != nil {
		t.Fatalf("ssh-keygen -y: %v", err)
	}
	laptopPub := strings.TrimSpace(string(pubOut)) + " athome@drillhost"
	if err := os.WriteFile(laptop+".pub", []byte(laptopPub+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := Machine{
		Name: "drill", Scope: "work/drill", Target: host, User: "dev", Port: portN,
		Athome: "v0.1.0", Home: "/home/dev", Hostname: "drillbox",
	}
	if err := Save(home, []Machine{m}); err != nil {
		t.Fatal(err)
	}
	if err := Refresh(home, filepath.Base(laptop)); err != nil {
		t.Fatal(err)
	}
	// The temp home's ~/.ssh/config pulls in the generated config by absolute
	// path, because ssh resolves a relative Include against the real home.
	include := "Include " + filepath.Join(SSHDir(home), "config") + "\n"
	if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(include), 0o600); err != nil {
		t.Fatal(err)
	}
	scanHostKey(t, home, host, port)

	d := &drillRunner{t: t, home: home, host: laptopHost, real: run.Exec(), items: map[string]string{}}
	r := Remote{Run: d, Home: home, Host: laptopHost}
	box := NewBox(r, m)

	login := func(key string) error { return r.LoginCheck(ctx, m, key) }
	mustLogin := func(what, key string) {
		t.Helper()
		if err := login(key); err != nil {
			t.Fatalf("%s should log in: %v", what, err)
		}
	}
	mustFail := func(what, key string) {
		t.Helper()
		if err := login(key); err == nil {
			t.Fatalf("%s should not log in", what)
		}
	}

	t.Run("authorize", func(t *testing.T) {
		mustLogin("the first key", laptop)
		extra := filepath.Join(t.TempDir(), "extra")
		extraPub := genKey(t, extra)
		mustFail("a key not yet authorized", extra)
		if err := box.Authorize(ctx, extraPub); err != nil {
			t.Fatal(err)
		}
		mustLogin("the authorized key", extra)
		// Authorizing twice adds nothing.
		if err := box.Authorize(ctx, extraPub); err != nil {
			t.Fatal(err)
		}
		res, err := r.Exec(ctx, "drill", `grep -c "$1" "$HOME/.ssh/authorized_keys"`, "", strings.Fields(extraPub)[1])
		if err != nil || strings.TrimSpace(res.Stdout) != "1" {
			t.Fatalf("authorize twice: count %q, err %v", res.Stdout, err)
		}
	})

	t.Run("reset keeps the laptop and break-glass keys only", func(t *testing.T) {
		hand := filepath.Join(t.TempDir(), "hand")
		handPub := genKey(t, hand)
		if err := box.Authorize(ctx, handPub); err != nil {
			t.Fatal(err)
		}
		mustLogin("the hand-added key", hand)

		bgPub, err := keys.NewBreakGlass(ctx, d, m.Scope)
		if err != nil {
			t.Fatal(err)
		}
		if err := box.Reset(ctx, []string{laptopPub, bgPub}); err != nil {
			t.Fatal(err)
		}
		mustFail("the hand-added key after reset", hand)
		mustLogin("the laptop key after reset", laptop)
		bg := filepath.Join(t.TempDir(), "bg")
		if err := os.WriteFile(bg, []byte(d.notes(keys.BreakGlassItem(m.Scope))), 0o600); err != nil {
			t.Fatal(err)
		}
		mustLogin("the break-glass key after reset", bg)
		res, err := r.Exec(ctx, "drill", `stat -c %a "$HOME/.ssh/authorized_keys"`, "")
		if err != nil || strings.TrimSpace(res.Stdout) != "600" {
			t.Fatalf("authorized_keys mode %q, err %v", res.Stdout, err)
		}
	})

	t.Run("laptop rotation", func(t *testing.T) {
		oldCopy := filepath.Join(t.TempDir(), "old")
		oldPriv, err := os.ReadFile(laptop)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(oldCopy, oldPriv, 0o600); err != nil {
			t.Fatal(err)
		}

		old := keys.Inventory
		t.Cleanup(func() { keys.Inventory = old })
		// KeysInventory insists on "Include athome/config"; the drill's include is an
		// absolute path (see above), so it builds the boxes itself.
		keys.Inventory = func(h string) ([]keys.Box, error) {
			ms, err := Load(h)
			if err != nil {
				return nil, err
			}
			var out []keys.Box
			for _, m := range ms {
				out = append(out, NewBox(Remote{Run: d, Home: h, Host: laptopHost}, m))
			}
			return out, nil
		}
		o := keys.Options{Home: home, Host: laptopHost, Run: d, Out: io.Discard, Now: time.Now}
		if err := keys.Rotate(ctx, o, keys.Kinds{Laptop: true}); err != nil {
			t.Fatal(err)
		}

		mustFail("the old laptop key after rotation", oldCopy)
		mustLogin("the new laptop key after rotation", laptop)
		bg := filepath.Join(t.TempDir(), "bg")
		if err := os.WriteFile(bg, []byte(d.notes(keys.BreakGlassItem(m.Scope))), 0o600); err != nil {
			t.Fatal(err)
		}
		mustLogin("the break-glass key after rotation", bg)
		if _, err := os.Stat(laptop + ".old"); err == nil {
			t.Error("the old laptop key file is still there after a finished rotation")
		}
	})
}
