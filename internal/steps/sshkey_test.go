package steps_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/run/runtest"
)

const pubLine = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIderived athome@box\n"

// backup builds an --import-from dir holding ssh/<name>_ed25519 (+ .pub).
func backup(t *testing.T, name, priv, pub string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ssh", name+"_ed25519"), priv, 0o600)
	if pub != "" {
		writeFile(t, filepath.Join(dir, "ssh", name+"_ed25519.pub"), pub, 0o644)
	}
	return dir
}

func keyPath(fx *fixture) string { return fx.Path(".ssh", testHost+"_ed25519") }

func stubDerive(fx *fixture) {
	fx.Env.Run = deriveSim{fx.Env.Run, fx.Fake}
	fx.Fake.On("ssh-keygen", "-y", "-f", keyPath(fx)).Return(run.Result{Stdout: pubLine})
}

// flakyDerive fails every `ssh-keygen -y` while fail is set.
type flakyDerive struct {
	run.Runner
	fail *bool
}

func (f flakyDerive) Run(ctx context.Context, c run.Cmd) (run.Result, error) {
	res, err := f.Runner.Run(ctx, c)
	if c.Name == "ssh-keygen" && containsSeq(c.Args, "-y") && *f.fail {
		return run.Result{Code: 1, Stderr: "invalid format"}, nil
	}
	return res, err
}

// deriveSim answers the interactive derive, `sh -c 'exec ssh-keygen -y -f "$1" > "$2"' athome-derive KEY OUT`,
// from the fake's `ssh-keygen -y -f KEY` rule: it records that call as ssh-keygen,
// with the Interactive flag, and writes the rule's stdout to OUT.
type deriveSim struct {
	run.Runner
	fake *runtest.Fake
}

func (d deriveSim) Run(ctx context.Context, c run.Cmd) (run.Result, error) {
	if c.Name == "sh" && len(c.Args) == 5 && c.Args[2] == "athome-derive" {
		res, _ := d.fake.Run(ctx, run.Cmd{Name: "ssh-keygen", Args: []string{"-y", "-f", c.Args[3]}, Interactive: c.Interactive})
		if res.Code == 0 {
			os.WriteFile(c.Args[4], []byte(res.Stdout), 0o600)
		}
		return run.Result{Code: res.Code}, nil
	}
	return d.Runner.Run(ctx, c)
}

func generateCalls(fx *fixture) []run.Cmd { return fx.callsTo("ssh-keygen", "-t", "ed25519") }
