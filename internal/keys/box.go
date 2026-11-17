package keys

import "context"

// Box is one inventory machine as rotation sees it. The remote-machines build
// implements it over ssh. An error from any method skips the box: it is listed,
// keeps its old key, and `athome keys rotate --resume` retries it.
//
// An ssh-backed Box must connect with <laptop key>.old while that file exists
// (a box skipped after the laptop key swap still accepts only the old key),
// then fall back to the laptop key.
type Box interface {
	Name() string
	Scope() string
	// Authorize appends pub to the box's authorized_keys.
	Authorize(ctx context.Context, pub string) error
	// Login proves the private key at keyPath logs in to the box.
	Login(ctx context.Context, keyPath string) error
	// Reset replaces authorized_keys with exactly the managed public keys.
	Reset(ctx context.Context, managed []string) error
}

// Inventory lists the boxes. Until the remote-machines build replaces it with
// the inventory reader, there are none, so rotation reaches GitHub and
// Bitwarden only.
var Inventory = func(home string) ([]Box, error) { return nil, nil }
