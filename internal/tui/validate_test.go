package tui

import (
	"context"
	"errors"
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
	v := validHandle(func(context.Context, string) error { calls++; return errors.New("unknown") }, context.Background())
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
	v := validBitwardenURL(func(context.Context, string) error { return errors.New("down") }, context.Background())
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
