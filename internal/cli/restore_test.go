package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/run/runtest"
	"github.com/morrieinmaas/athome/internal/steps"
)

func ptr(f func() []steps.Step) uintptr { return reflect.ValueOf(f).Pointer() }

// initList runs runInitCmd against a temp home and repo with a fake runner and
// reports which step list the run selected. The run is cancelled before its
// first step, so nothing but the seam is exercised.
func initList(t *testing.T, args ...string) (selected uintptr, after uintptr, code int, stderr string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("PATH", os.Getenv("PATH"))
	t.Setenv("TMPDIR", t.TempDir())
	for _, k := range []string{"GITHUB_TOKEN", "ATHOME_CI", "ATHOME_GITHUB_HANDLE"} {
		t.Setenv(k, "")
	}
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "home", ".chezmoiscripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".chezmoiroot"), []byte("home\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATHOME_REPO", repo)

	oldCtx, oldRunner := notifyContext, newRunner
	t.Cleanup(func() { notifyContext, newRunner = oldCtx, oldRunner })
	f := runtest.NewFake()
	newRunner = func() run.Runner { return f }
	notifyContext = func(parent context.Context, _ ...os.Signal) (context.Context, context.CancelFunc) {
		selected = ptr(stepList)
		ctx, cancel := context.WithCancel(parent)
		cancel()
		return ctx, cancel
	}
	var o, e bytes.Buffer
	code = runInitCmd(args, &o, &e)
	return selected, ptr(stepList), code, e.String()
}

func TestRestoreSelectsRestoreAllForTheRunOnly(t *testing.T) {
	before := ptr(stepList)
	sel, after, _, errOut := initList(t, "--non-interactive", "--machine", "personal", "--restore")
	if sel != ptr(steps.RestoreAll) {
		t.Fatalf("the run did not use RestoreAll (stderr %q)", errOut)
	}
	if after != before {
		t.Errorf("stepList was not restored after the run")
	}
}

func TestInitWithoutRestoreKeepsTheList(t *testing.T) {
	sel, _, _, errOut := initList(t, "--non-interactive", "--machine", "personal")
	if sel != ptr(steps.All) {
		t.Fatalf("a plain init must use steps.All (stderr %q)", errOut)
	}
}

func TestStripRestore(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		want    []string
		restore bool
	}{
		{"plain", []string{"--restore"}, nil, true},
		{"among flags", []string{"--yes", "--restore", "--machine", "work"}, []string{"--yes", "--machine", "work"}, true},
		{"ref x then restore", []string{"--ref", "x", "--restore"}, []string{"--ref", "x"}, true},
		{"restore is the ref value", []string{"--ref", "--restore"}, []string{"--ref", "--restore"}, false},
		{"import value", []string{"--import-ssh-bw", "--restore", "--restore"}, []string{"--import-ssh-bw", "--restore"}, true},
		{"none", []string{"--yes"}, []string{"--yes"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rest, restore, _ := stripRestore(c.args)
			if !reflect.DeepEqual(rest, c.want) || restore != c.restore {
				t.Errorf("stripRestore(%q) = %q, %v; want %q, %v", c.args, rest, restore, c.want, c.restore)
			}
		})
	}
}

func TestInitHelpNamesRestore(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		var o, e bytes.Buffer
		if code := runInitCmd([]string{flag}, &o, &e); code != exitOK {
			t.Fatalf("%s exit = %d", flag, code)
		}
		if !strings.Contains(o.String(), "  --restore\t") {
			t.Errorf("%s output lacks --restore:\n%s", flag, o.String())
		}
		if !strings.Contains(o.String(), "  --ref\t") {
			t.Errorf("%s output lost the other flags:\n%s", flag, o.String())
		}
	}
}
