package tui

import (
	"context"
	"errors"
	"regexp"
	"strings"
)

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

// overridable wraps a network check so that a failure is shown once and a
// second submit of the same value is accepted (offline machines must still init).
func overridable(check func(string) error) func(string) error {
	failed := ""
	hasFailed := false
	return func(v string) error {
		if hasFailed && v == failed {
			return nil
		}
		if err := check(v); err != nil {
			failed, hasFailed = v, true
			return errors.New(err.Error() + " (press enter again to use it anyway)")
		}
		hasFailed = false
		return nil
	}
}
