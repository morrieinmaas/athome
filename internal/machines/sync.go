package machines

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const envCopyScript = `umask 077; mkdir -p "$HOME/.secrets/$1" && cat > "$HOME/.secrets/$1/$2"`

// copyEnv sends ~/.secrets/<repo>/.env* (not .env.example) to the box, on stdin.
func copyEnv(ctx context.Context, d Deps, exec execFn, m Machine, repo string, out io.Writer) error {
	files, err := filepath.Glob(filepath.Join(d.Home, ".secrets", repo, ".env*"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, f := range files {
		base := filepath.Base(f)
		if base == ".env.example" {
			continue
		}
		if fi, err := os.Stat(f); err != nil || !fi.Mode().IsRegular() {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		res, err := exec(ctx, m.Name, envCopyScript, string(b), repo, base)
		if err := okOrErr("copy "+base, m.Name, res, err); err != nil {
			return err
		}
		fmt.Fprintf(out, "copied %s/%s to %s\n", repo, base, m.Name)
	}
	return nil
}

// syncBox reruns the bootstrap step, then copies the .env files of the machine's repos.
func syncBox(ctx context.Context, d Deps, m Machine, interactive bool, out io.Writer) error {
	if err := bootstrapBox(ctx, d, m, interactive, out); err != nil {
		return err
	}
	exec := execFn(d.remote().Exec)
	if !interactive {
		exec = execNoTTY(d.remote())
	}
	for _, repo := range m.Repos {
		if err := copyEnv(ctx, d, exec, m, repo, out); err != nil {
			return err
		}
	}
	return nil
}

// Sync reruns the bootstrap on one machine (interactively) or on every machine
// in parallel without a terminal, then copies .env files for the listed repos.
func Sync(ctx context.Context, d Deps, name string, all bool) error {
	if (name == "") == !all {
		return UsageError("usage: athome machine sync <name> | --all")
	}
	ms, err := Load(d.Home)
	if err != nil {
		return err
	}
	if all && len(ms) == 0 {
		d.say("no machines")
		return nil
	}
	if err := d.boxConfig(); err != nil {
		return err
	}
	if all {
		return syncAll(ctx, d, ms)
	}
	return syncOne(ctx, d, ms, name)
}

// syncOne syncs the machine called name, interactively.
func syncOne(ctx context.Context, d Deps, ms []Machine, name string) error {
	for _, m := range ms {
		if m.Name == name {
			if err := syncBox(ctx, d, m, true, d.outOrDiscard()); err != nil {
				return err
			}
			d.say("✓ %s", name)
			return nil
		}
	}
	return fmt.Errorf("machine %s not found", name)
}

// syncAll syncs every machine in parallel, each into its own log, and prints
// one line per machine in inventory order.
func syncAll(ctx context.Context, d Deps, ms []Machine) error {
	lines := make([]string, len(ms))
	var wg sync.WaitGroup
	for i, m := range ms {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lines[i] = syncLogged(ctx, d, m)
		}()
	}
	wg.Wait()
	failed := 0
	for _, l := range lines {
		d.say("%s", l)
		if strings.HasPrefix(l, "✗") {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("sync failed on %d of %d machines", failed, len(ms))
	}
	return nil
}

// syncLogged syncs m without a terminal into ~/.local/state/athome/sync-<name>.log
// and returns its summary line.
func syncLogged(ctx context.Context, d Deps, m Machine) string {
	var buf bytes.Buffer
	err := syncBox(ctx, d, m, false, &buf)
	if err != nil {
		fmt.Fprintf(&buf, "error: %v\n", err)
	}
	log := filepath.Join(d.Home, ".local", "state", "athome", "sync-"+m.Name+".log")
	if werr := writeAtomic(log, buf.Bytes()); werr != nil && err == nil {
		err = fmt.Errorf("write log: %w", werr)
	}
	if err != nil {
		return fmt.Sprintf("✗ %s: %v (log: %s)", m.Name, firstLine(err.Error()), log)
	}
	return "✓ " + m.Name
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}
