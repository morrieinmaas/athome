package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestValidEmail(t *testing.T) {
	for s, want := range map[string]bool{"a@b.co": true, "a@b": false, "a b@c.d": false, "": false, "x@y@z.com": false} {
		if got := ValidEmail(s); got != want {
			t.Errorf("ValidEmail(%q)=%v want %v", s, got, want)
		}
	}
	if validOptionalEmail("") != nil || validOptionalEmail("a@b") == nil || validOptionalEmail("a@b.co") != nil {
		t.Error("optional email rules wrong")
	}
}

func TestValidHandleRequiredAndCheckOnce(t *testing.T) {
	calls := 0
	v := validHandle(func(context.Context, string) error { calls++; return ErrCheckUnavailable }, context.Background())
	if v("") == nil {
		t.Error("empty handle accepted")
	}
	if v("nobody") == nil {
		t.Fatal("first submit should fail")
	}
	if err := v("nobody"); err != nil {
		t.Errorf("second submit of same value rejected: %v", err)
	}
	if v("other") == nil {
		t.Error("a different value must be checked again")
	}
	if calls != 2 {
		t.Errorf("calls=%d want 2", calls)
	}
}

func TestValidBitwardenURL(t *testing.T) {
	v := validBitwardenURL(func(context.Context, string) error { return ErrCheckUnavailable }, context.Background())
	if v("") != nil {
		t.Error("empty must be accepted without a check")
	}
	if v("http://x") == nil {
		t.Error("non-https accepted")
	}
	if v("https://vault.example") == nil || v("https://vault.example") != nil {
		t.Error("check failure should show once then accept")
	}
}

// huh validates on enter and on blur; a value that passed must not be checked
// over the network a second time.
func TestPassingCheckIsCached(t *testing.T) {
	calls := 0
	ok := func(context.Context, string) error { calls++; return nil }
	h := validHandle(ok, context.Background())
	h("octo")
	h("octo")
	u := validBitwardenURL(ok, context.Background())
	u("https://vault.example")
	u("https://vault.example")
	if calls != 2 {
		t.Errorf("calls = %d, want one per distinct valid value (2)", calls)
	}
	h("other")
	if calls != 3 {
		t.Errorf("a new value must be checked: calls = %d", calls)
	}
}

// Only an unavailable network may be overridden; any other failure keeps blocking.
func TestOnlyUnavailableChecksAreOverridable(t *testing.T) {
	bad := errors.New(`no GitHub user "nobody"`)
	h := validHandle(func(context.Context, string) error { return bad }, context.Background())
	for i := 0; i < 3; i++ {
		if h("nobody") == nil {
			t.Fatalf("submit %d: a definite failure must keep blocking", i+1)
		}
	}
	down := fmt.Errorf("%w x: offline", ErrCheckUnavailable)
	h = validHandle(func(context.Context, string) error { return down }, context.Background())
	if h("octo") == nil {
		t.Fatal("first submit should show the failure")
	}
	if err := h("octo"); err != nil {
		t.Errorf("an unavailable check must pass on the second submit: %v", err)
	}
	u := validBitwardenURL(func(context.Context, string) error { return bad }, context.Background())
	if u("https://x.example") == nil || u("https://x.example") == nil {
		t.Error("a definite URL failure must keep blocking")
	}
}

func TestValidHostname(t *testing.T) {
	for s, ok := range map[string]bool{
		"": true, "box": true, "my-box-2": true, "A1": true,
		"a b": false, "../x": false, "x/y": false, "-x": false, "x_y": false, "x.y": false,
		strings.Repeat("a", 63): true, strings.Repeat("a", 64): false,
	} {
		if got := validHostname(s) == nil; got != ok {
			t.Errorf("validHostname(%q) accepted=%v, want %v", s, got, ok)
		}
	}
}
