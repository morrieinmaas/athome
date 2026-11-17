package tui

import (
	"context"
	"errors"
	"regexp"
	"strings"
)

// ErrCheckUnavailable marks a network check that could not run (transport error
// or an unexpected status). Only such a failure may be overridden by the user.
var ErrCheckUnavailable = errors.New("could not check")

var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// ValidEmail reports whether s has the shape of an email address. Empty is not
// valid here: fields that allow it check for empty first.
func ValidEmail(s string) bool { return emailRe.MatchString(s) }

func validOptionalEmail(s string) error {
	if s == "" || ValidEmail(s) {
		return nil
	}
	return errors.New("not an email address (want name@host.tld)")
}

func validHandle(check func(context.Context, string) error, ctx context.Context) func(string) error {
	once := overridable(func(v string) error {
		if check == nil {
			return nil
		}
		return check(ctx, v)
	})
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New("a GitHub handle is required")
		}
		return once(s)
	}
}

func validBitwardenURL(check func(context.Context, string) error, ctx context.Context) func(string) error {
	once := overridable(func(v string) error {
		if check == nil {
			return nil
		}
		return check(ctx, v)
	})
	return func(s string) error {
		if s == "" {
			return nil
		}
		if !strings.HasPrefix(s, "https://") {
			return errors.New("must be empty or start with https://")
		}
		return once(s)
	}
}

// overridable wraps a network check. A value that passed is not checked again
// (huh validates on enter and on blur). A failure that wraps ErrCheckUnavailable
// is shown once: a second submit of the same value is accepted (offline machines
// must still init). Any other failure blocks every time.
func overridable(check func(string) error) func(string) error {
	last := ""
	has := false
	return func(v string) error {
		if has && v == last {
			return nil
		}
		err := check(v)
		if err == nil {
			last, has = v, true
			return nil
		}
		if !errors.Is(err, ErrCheckUnavailable) {
			return err
		}
		last, has = v, true
		return errors.New(err.Error() + " (press enter again to use it anyway)")
	}
}

var hostnameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,62}$`)

// validHostname allows empty (the default); the name ends up in ~/.ssh/<hostname>_ed25519.
func validHostname(s string) error {
	if s == "" || hostnameRe.MatchString(s) {
		return nil
	}
	return errors.New("use letters, digits and hyphens only, starting with a letter or digit (max 63)")
}
