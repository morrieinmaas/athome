package machines

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSecret(t *testing.T, home, repo, file, content string) {
	t.Helper()
	dir := filepath.Join(home, ".secrets", repo)
	os.MkdirAll(dir, 0o700)
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSyncOneBoxRunsInteractivelyAndCopiesListedRepos(t *testing.T) {
	w, d, out := newWorld(t)
	if err := Save(d.Home, []Machine{acme()}); err != nil { // repos = ["api"]
		t.Fatal(err)
	}
	writeSecret(t, d.Home, "api", ".env", "A=1\n")
	writeSecret(t, d.Home, "api", ".env.local", "B=2\n")
	writeSecret(t, d.Home, "api", ".env.example", "X=\n")
	writeSecret(t, d.Home, "other", ".env", "NOPE=1\n")

	if err := Sync(t.Context(), d, "acme-dev", false); err != nil {
		t.Fatal(err)
	}
	if got := w.ops("box-upload-bootstrap"); len(got) != 1 {
		t.Errorf("uploads: %v", got)
	}
	if got := w.ops("box-bootstrap acme-dev v0.4.0(interactive)"); len(got) != 1 {
		t.Errorf("bootstrap: %v", w.logCopy())
	}
	if got := w.ops("box-env"); len(got) != 2 || got[0] != "box-env acme-dev api/.env" || got[1] != "box-env acme-dev api/.env.local" {
		t.Errorf("env copies: %v", got)
	}
	if w.boxes["acme-dev"].envs["api/.env.local"] != "B=2\n" {
		t.Errorf("content: %v", w.boxes["acme-dev"].envs)
	}
	if !strings.Contains(out.String(), "✓ acme-dev") {
		t.Errorf("out: %s", out)
	}
	sawT := false
	for _, a := range w.sshArgs() {
		if strings.HasPrefix(a, "-t ") {
			sawT = true
		}
	}
	if !sawT {
		t.Errorf("no ssh -t call: %v", w.sshArgs())
	}
	// data never travels in args
	for _, a := range w.sshArgs() {
		if strings.Contains(a, "A=1") {
			t.Errorf("secret in args: %s", a)
		}
	}
}

func TestSyncRerunsEvenWhenCurrent(t *testing.T) {
	w, d, _ := newWorld(t)
	Save(d.Home, []Machine{acme()})
	for i := 0; i < 2; i++ {
		if err := Sync(t.Context(), d, "acme-dev", false); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(w.ops("box-bootstrap")); n != 2 {
		t.Errorf("bootstrap ran %d times", n)
	}
}

func TestSyncUsage(t *testing.T) {
	_, d, _ := newWorld(t)
	Save(d.Home, []Machine{acme()})
	for _, c := range []struct {
		name string
		all  bool
	}{{"", false}, {"acme-dev", true}} {
		err := Sync(t.Context(), d, c.name, c.all)
		var u UsageError
		if !errors.As(err, &u) {
			t.Errorf("Sync(%q, %v) = %v, want a UsageError", c.name, c.all, err)
		}
	}
	if err := Sync(t.Context(), d, "nope", false); err == nil || errors.As(err, new(UsageError)) {
		t.Errorf("unknown name: %v", err)
	}
}

func TestSyncAllMixedResult(t *testing.T) {
	w, d, out := newWorld(t)
	gp := gpu()
	gp.Home, gp.Hostname = "/home/ops", "ops"
	if err := Save(d.Home, []Machine{acme(), gp}); err != nil {
		t.Fatal(err)
	}
	w.box("acme-gpu").down = true
	writeSecret(t, d.Home, "api", ".env", "A=1\n")

	err := Sync(t.Context(), d, "", true)
	if err == nil {
		t.Fatal("exit must be non-zero when a box fails")
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	last := lines[len(lines)-2:]
	logGPU := filepath.Join(d.Home, ".local", "state", "athome", "sync-acme-gpu.log")
	if last[0] != "✓ acme-dev" || !strings.HasPrefix(last[1], "✗ acme-gpu: ") || !strings.HasSuffix(last[1], " (log: "+logGPU+")") {
		t.Fatalf("final lines: %q", last)
	}
	for _, name := range []string{"acme-dev", "acme-gpu"} {
		fi, err := os.Stat(filepath.Join(d.Home, ".local", "state", "athome", "sync-"+name+".log"))
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("log %s: %v %v", name, fi, err)
		}
	}
	if got := w.ops("box-bootstrap acme-dev v0.4.0 --non-interactive"); len(got) != 1 {
		t.Errorf("non-interactive bootstrap: %v", w.logCopy())
	}
	for _, a := range w.sshArgs() {
		if strings.Contains(a, "-t ") {
			t.Errorf("a terminal was requested in --all: %s", a)
		}
	}
	if b, _ := os.ReadFile(logGPU); !strings.Contains(string(b), "No route to host") {
		t.Errorf("failing box log: %q", b)
	}
}

func TestSyncAllEmpty(t *testing.T) {
	_, d, out := newWorld(t)
	if err := Sync(t.Context(), d, "", true); err != nil || out.String() != "no machines\n" {
		t.Fatalf("%v %q", err, out)
	}
}
