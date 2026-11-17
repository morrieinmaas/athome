package steps_test

import (
	"context"
	"testing"

	"github.com/morrieinmaas/athome/internal/steps"
)

// A hostname answered in the TUI lives in chezmoi.toml; a later run with no
// answer (-y, --plain, --config) keeps it and looks for that host's key.
func TestStoredHostnameSurvivesANonInteractiveRun(t *testing.T) {
	fx := newFixture(t)
	t.Setenv("HOSTNAME", "envhost")
	writeFile(t, chezmoiTomlPath(fx), "[data]\n  hostname = \"box\"\n", 0o600)
	fx.Env.Cfg.Data = map[string]string{"githubHandle": "octo", "githubId": "1"}
	fx.MustExec(steps.Data())
	if got := readChezmoiData(t, chezmoiTomlPath(fx))["hostname"]; got != "box" {
		t.Errorf("hostname = %#v, want box", got)
	}
	if got, want := steps.KeyPath(context.Background(), fx.Env), fx.Path(".ssh", "box_ed25519"); got != want {
		t.Errorf("KeyPath = %q, want %q", got, want)
	}
}
