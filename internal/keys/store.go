// Package keys rotates the laptop's ssh key and the break-glass keys, and
// stores keys in Bitwarden. Every external tool goes through run.Runner.
package keys

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/morrieinmaas/athome/internal/run"
)

// SSHKeyPassword is the password field of every ssh key item; the private key
// is in the notes, which is what `athome init --import-ssh-bw` reads.
const SSHKeyPassword = "ssh-key-backup"

// LaptopKey is the key this laptop uses: ~/.ssh/id_ed25519 when that regular
// file exists, else ~/.ssh/<host>_ed25519.
func LaptopKey(home, host string) string {
	id := filepath.Join(home, ".ssh", "id_ed25519")
	if fi, err := os.Lstat(id); err == nil && fi.Mode().IsRegular() {
		return id
	}
	return filepath.Join(home, ".ssh", host+"_ed25519")
}

// ItemFor is the Bitwarden item that holds a key file.
func ItemFor(key string) string { return "ssh/" + filepath.Base(key) }

// ErrLocked is returned when rbw is not unlocked.
var ErrLocked = errors.New("rbw is locked; run: rbw unlock, then re-run")

// ErrNoRbw is returned when rbw is not installed.
var ErrNoRbw = errors.New("rbw is not installed; install it (athome init does), then re-run")

// Store writes password and notes to the Bitwarden item, adding it when absent
// and editing it in place when present, so a name never has two items. The
// content reaches rbw through an EDITOR script, never on a command line.
func Store(ctx context.Context, r run.Runner, item, password, notes string) error {
	if _, err := r.LookPath("rbw"); err != nil {
		return ErrNoRbw
	}
	if res, err := r.Run(ctx, run.Cmd{Name: "rbw", Args: []string{"unlocked"}}); err != nil || res.Code != 0 {
		return ErrLocked
	}
	res, err := r.Run(ctx, run.Cmd{Name: "rbw", Args: []string{"list"}})
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("rbw list failed: %s", strings.TrimSpace(res.Stderr))
	}
	n := 0
	for _, l := range strings.Split(res.Stdout, "\n") {
		if strings.TrimSpace(l) == item {
			n++
		}
	}
	verb := "add"
	switch {
	case n == 1:
		verb = "edit"
	case n > 1:
		return fmt.Errorf("Bitwarden has %d items named %s; delete the extras, then re-run", n, item)
	}

	dir, err := os.MkdirTemp("", "athome-rbw-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	content := filepath.Join(dir, "content")
	if err := os.WriteFile(content, []byte(password+"\n"+notes+"\n"), 0o600); err != nil {
		return err
	}
	editor := filepath.Join(dir, "editor")
	script := "#!/bin/sh\ncat " + shellQuote(content) + " > \"$1\"\n"
	if err := os.WriteFile(editor, []byte(script), 0o700); err != nil {
		return err
	}
	res, err = r.Run(ctx, run.Cmd{
		Name: "rbw", Args: []string{verb, item},
		Env: []string{"EDITOR=" + editor, "VISUAL=" + editor},
	})
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("rbw %s %s failed: %s", verb, item, strings.TrimSpace(res.Stderr))
	}
	return nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
