package secrets

import (
	"os"
	"path/filepath"
	"syscall"
)

// lock takes an exclusive lock on <home>/.local/state/athome/secrets.lock, so
// backup and rekey never run at once. It returns false when another run holds
// it. The returned func releases it.
func (o Opts) lock() (release func(), ok bool, err error) {
	p := filepath.Join(o.Home, ".local", "state", "athome", "secrets.lock")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, false, err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() { f.Close() }, true, nil
}

// acquire is lock with the failure reported; ok is false when the run must stop.
func (o Opts) acquire() (release func(), ok bool) {
	release, got, err := o.lock()
	if err != nil {
		o.errf("athome secrets: could not take the lock: %v\n", err)
		return nil, false
	}
	if !got {
		o.errf("athome secrets: another secrets run is in progress; wait for it to finish, then re-run\n")
		return nil, false
	}
	return release, true
}
