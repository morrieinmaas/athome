package steps_test

import (
	"context"
	"testing"

	"github.com/morrieinmaas/athome/internal/steps"
)

func dataHostnameState(t *testing.T, fx *fixture) (string, any) {
	t.Helper()
	d := readChezmoiData(t, chezmoiTomlPath(fx))
	h, _ := d["hostname"].(string)
	return h, d["hostnameFromForm"]
}

func runDataWith(t *testing.T, fx *fixture, answers map[string]string, answered bool) {
	t.Helper()
	fx.Env.Cfg.Data = map[string]string{"githubHandle": "octo", "githubId": "1"}
	for k, v := range answers {
		fx.Env.Cfg.Data[k] = v
	}
	fx.Env.Answered = nil
	if answered {
		fx.Env.Answered = map[string]bool{"hostname": true}
	}
	fx.MustExec(steps.Data())
}

// A chezmoi.toml from build 2 has a hostname but no flag: a non-interactive run
// writes the short hostname again (B:881); the key path ignores the stale one.
func TestBuild2StoredHostnameIsRewrittenAsShort(t *testing.T) {
	fx := newFixture(t)
	t.Setenv("HOSTNAME", "envhost")
	writeFile(t, chezmoiTomlPath(fx), "[data]\n  hostname = \"old\"\n", 0o600)
	runDataWith(t, fx, nil, false)
	if h, flag := dataHostnameState(t, fx); h != "envhost" || flag != nil {
		t.Errorf("hostname = %q flag = %v, want envhost and no flag", h, flag)
	}
	if got, want := steps.KeyPath(context.Background(), fx.Env), fx.Path(".ssh", testHost+"_ed25519"); got != want { // chezmoi's hostname, as in build 2
		t.Errorf("KeyPath = %q, want %q", got, want)
	}
}

// A hostname the form chose survives later non-interactive runs.
func TestFormHostnamePersistsAcrossANonInteractiveRun(t *testing.T) {
	fx := newFixture(t)
	t.Setenv("HOSTNAME", "envhost")
	runDataWith(t, fx, map[string]string{"hostname": "box"}, true)
	if h, flag := dataHostnameState(t, fx); h != "box" || flag != true {
		t.Fatalf("after the form: hostname = %q flag = %v, want box and true", h, flag)
	}
	runDataWith(t, fx, nil, false)
	if h, flag := dataHostnameState(t, fx); h != "box" || flag != true {
		t.Errorf("after -y: hostname = %q flag = %v, want box and true", h, flag)
	}
	if got, want := steps.KeyPath(context.Background(), fx.Env), fx.Path(".ssh", "box_ed25519"); got != want {
		t.Errorf("KeyPath = %q, want %q", got, want)
	}
}

// The form can set the hostname back to the short one.
func TestFormCanResetHostnameToTheShortOne(t *testing.T) {
	fx := newFixture(t)
	t.Setenv("HOSTNAME", "envhost")
	writeFile(t, chezmoiTomlPath(fx), "[data]\n  hostname = \"box\"\n  hostnameFromForm = true\n", 0o600)
	runDataWith(t, fx, map[string]string{"hostname": "envhost"}, true)
	if h, flag := dataHostnameState(t, fx); h != "envhost" || flag != nil {
		t.Errorf("hostname = %q flag = %v, want envhost and no flag", h, flag)
	}
	runDataWith(t, fx, nil, false)
	if h, _ := dataHostnameState(t, fx); h != "envhost" {
		t.Errorf("after -y: hostname = %q, want envhost", h)
	}
}
