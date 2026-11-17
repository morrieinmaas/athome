package cli_test

import (
	"bytes"
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
