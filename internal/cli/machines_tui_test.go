package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/machines"
	"github.com/morrieinmaas/athome/internal/tui"
)

type machineCalls struct{ calls []string }

func machinesSandbox(t *testing.T, res tui.MachinesResult) (*machineCalls, *tui.MachinesInfo) {
	t.Helper()
	home, _ := tuiSandbox(t)
	chez := filepath.Join(home, ".config", "chezmoi")
	os.MkdirAll(chez, 0o755)
	os.WriteFile(filepath.Join(chez, "chezmoi.toml"), []byte("[data]\n  machineType = \"work\"\n"), 0o644)
	if err := machines.Save(home, []machines.Machine{{Name: "box", Scope: "work/x", Target: "h", User: "u", Port: 22, Athome: "v0.1.0"}}); err != nil {
		t.Fatal(err)
	}
	c := &machineCalls{}
	info := &tui.MachinesInfo{}
	oldView, oldAdd, oldSync, oldRm, oldHome := runMachinesView, machinesAdd, machinesSync, machinesRemove, runHome
	t.Cleanup(func() {
		runMachinesView, machinesAdd, machinesSync, machinesRemove, runHome = oldView, oldAdd, oldSync, oldRm, oldHome
	})
	runHome = func(io.Reader, io.Writer, tui.HomeInfo) (string, error) { return "machines", nil }
	runMachinesView = func(_ context.Context, _ io.Reader, _ io.Writer, i tui.MachinesInfo) (tui.MachinesResult, error) {
		*info = i
		return res, nil
	}
	machinesAdd = func(_ context.Context, _ machines.Deps, m machines.Machine) error {
		c.calls = append(c.calls, "add "+m.Name+" "+m.Athome)
		return nil
	}
	machinesSync = func(_ context.Context, _ machines.Deps, name string, all bool) error {
		c.calls = append(c.calls, "sync "+name+" "+map[bool]string{true: "all", false: "one"}[all])
		return nil
	}
	machinesRemove = func(_ context.Context, _ machines.Deps, name string, force bool) error {
		c.calls = append(c.calls, "remove "+name+" "+map[bool]string{true: "force", false: "safe"}[force])
		return nil
	}
	return c, info
}

func TestMachinesScreenDispatch(t *testing.T) {
	for _, c := range []struct {
		res  tui.MachinesResult
		want string
	}{
		{tui.MachinesResult{Action: "add", Machine: machines.Machine{Name: "n", Athome: "v1.0.0"}}, "add n v1.0.0"},
		{tui.MachinesResult{Action: "sync", Name: "box"}, "sync box one"},
		{tui.MachinesResult{Action: "sync all"}, "sync  all"},
		{tui.MachinesResult{Action: "remove", Name: "box"}, "remove box safe"},
	} {
		calls, info := machinesSandbox(t, c.res)
		if code, _, _ := run2(); code != 0 {
			t.Fatalf("%s: exit %d", c.want, code)
		}
		if len(calls.calls) != 1 || calls.calls[0] != c.want {
			t.Errorf("calls = %v, want %q", calls.calls, c.want)
		}
		if !strings.Contains(info.Listing, "box") || len(info.Names) != 1 || info.Names[0] != "box" {
			t.Errorf("info = %+v", info)
		}
	}
}

func TestMachinesScreenBackReturnsHome(t *testing.T) {
	calls, _ := machinesSandbox(t, tui.MachinesResult{Action: "back"})
	n := 0
	runHome = func(io.Reader, io.Writer, tui.HomeInfo) (string, error) {
		n++
		if n == 1 {
			return "machines", nil
		}
		return "quit", nil
	}
	if code, _, _ := run2(); code != 0 || n != 2 || len(calls.calls) != 0 {
		t.Errorf("exit %d home shown %d times, calls %v", code, n, calls.calls)
	}
}

func TestMachinesScreenErrorExits1(t *testing.T) {
	machinesSandbox(t, tui.MachinesResult{Action: "sync", Name: "box"})
	machinesSync = func(context.Context, machines.Deps, string, bool) error { return io.ErrUnexpectedEOF }
	if code, _, errOut := run2(); code != 1 || !strings.Contains(errOut, "unexpected EOF") {
		t.Errorf("exit %d err %q", code, errOut)
	}
}
