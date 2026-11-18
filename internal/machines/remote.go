package machines

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/morrieinmaas/athome/internal/keys"
	"github.com/morrieinmaas/athome/internal/run"
)

// Remote runs scripts on boxes through ssh. Host is the laptop's short hostname.
type Remote struct {
	Run  run.Runner
	Home string
	Host string
}

// ShQuote single-quotes s for a POSIX shell.
func ShQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (r Remote) laptopKey() string { return keys.LaptopKey(r.Home, r.Host) }

// sshArgs builds the ssh command line. While <laptop key>.old exists a box
// skipped after the key swap still accepts only that key.
func (r Remote) sshArgs(mode []string, name, script string, args []string) []string {
	out := slicesClone(mode)
	if old := r.laptopKey() + ".old"; fileExists(old) {
		out = append(out, "-i", old)
	}
	out = append(out, name, "sh", "-c", ShQuote(script), "athome")
	for _, a := range args {
		out = append(out, ShQuote(a))
	}
	return out
}

func slicesClone(s []string) []string { return append([]string(nil), s...) }

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Exec runs script on the box without a terminal. Data goes on stdin, never in args.
func (r Remote) Exec(ctx context.Context, name, script, stdin string, args ...string) (run.Result, error) {
	return r.Run.Run(ctx, run.Cmd{
		Name:  "ssh",
		Args:  r.sshArgs([]string{"-o", "BatchMode=yes"}, name, script, args),
		Stdin: stdin,
	})
}

// Interactive runs script on the box with a terminal, so it can prompt.
func (r Remote) Interactive(ctx context.Context, name, script string, args ...string) (run.Result, error) {
	return r.Run.Run(ctx, run.Cmd{
		Name:        "ssh",
		Args:        r.sshArgs([]string{"-t"}, name, script, args),
		Interactive: true,
	})
}

// LoginCheck proves key alone logs in to m: a temp config without the laptop
// key or a shared master connection, so neither can make it pass.
func (r Remote) LoginCheck(ctx context.Context, m Machine, key string) error {
	f, err := os.CreateTemp("", "athome-ssh-*.conf")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(block(m, "", true)); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	res, err := r.Run.Run(ctx, run.Cmd{Name: "ssh", Args: []string{
		"-F", f.Name(), "-i", key, "-o", "IdentitiesOnly=yes", "-o", "BatchMode=yes", m.Name, "true",
	}})
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("login to %s with %s failed: %s", m.Name, key, strings.TrimSpace(res.Stderr))
	}
	return nil
}
