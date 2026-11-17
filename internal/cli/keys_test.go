package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/cli"
	"github.com/morrieinmaas/athome/internal/keys"
	"github.com/morrieinmaas/athome/internal/run/runtest"
	"github.com/morrieinmaas/athome/internal/steps"
)

func runKeysMain(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	f := runtest.NewFake()
	f.Have("chezmoi")
	t.Cleanup(cli.SetRunner(f))
	var out, errw bytes.Buffer
	code := cli.Main(append([]string{"keys"}, args...), &out, &errw)
	return code, out.String(), errw.String()
}

func TestKeysUsage(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{nil, 2, "Usage: athome keys rotate"},
		{[]string{"bogus"}, 2, "Usage: athome keys rotate"},
		{[]string{"rotate"}, 2, "pick what to rotate"},
		{[]string{"rotate", "--resume", "--laptop"}, 2, "--resume takes no other flag"},
		{[]string{"rotate", "--bogus"}, 2, "unknown argument: --bogus"},
		{[]string{"--help"}, 0, "--break-glass"},
		{[]string{"rotate", "--resume"}, 0, "nothing to resume"},
		{[]string{"rotate", "--boxes"}, 0, "no machines in the inventory"},
		{[]string{"rotate", "--break-glass"}, 0, "no scopes in the inventory"},
		{[]string{"rotate", "--laptop"}, 1, "no laptop key"},
	} {
		code, out, errw := runKeysMain(t, tc.args...)
		if code != tc.code || !strings.Contains(out+errw, tc.want) {
			t.Errorf("keys %v = %d, %q %q; want %d with %q", tc.args, code, out, errw, tc.code, tc.want)
		}
	}
}

func TestUsageListsKeys(t *testing.T) {
	var out bytes.Buffer
	cli.Main([]string{"--help"}, &out, &out)
	if !strings.Contains(out.String(), "keys      rotate ssh keys") {
		t.Errorf("usage lacks keys:\n%s", out.String())
	}
}

func keysLaptopKey(home string) string {
	return keys.LaptopKey(home, steps.KeyHostname())
}

func TestKeysPendingAndAll(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TMPDIR", t.TempDir())
	f := runtest.NewFake()
	t.Cleanup(cli.SetRunner(f))
	state := keys.StatePath(home)
	os.MkdirAll(filepath.Dir(state), 0o755)
	os.WriteFile(state, []byte(`{"jobs":[{"kind":"laptop","targets":{"github":"added"}}]}`), 0o600)
	var out, errw bytes.Buffer
	if code := cli.Main([]string{"keys", "rotate", "--laptop"}, &out, &errw); code != 1 || !strings.Contains(errw.String(), "a rotation is unfinished") {
		t.Errorf("pending: %d %q", code, errw.String())
	}
	os.Remove(state)
	out.Reset()
	errw.Reset()
	// --all with no laptop key fails on the laptop job; --boxes --break-glass shows both notes.
	if code := cli.Main([]string{"keys", "rotate", "--boxes", "--break-glass"}, &out, &errw); code != 0 ||
		!strings.Contains(out.String(), "no machines in the inventory") || !strings.Contains(out.String(), "no scopes in the inventory") {
		t.Errorf("notes: %d %q %q", code, out.String(), errw.String())
	}
}
