package steps_test

import (
	"reflect"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
)

func TestOrder(t *testing.T) {
	all := steps.All()
	if len(all) == 0 {
		t.Fatal("All() is empty")
	}

	seen := map[string]bool{}
	for i, s := range all {
		if s.Name == "" {
			t.Errorf("step %d has no name", i)
		}
		if seen[s.Name] {
			t.Errorf("duplicate step name %q", s.Name)
		}
		seen[s.Name] = true
		if s.Check == nil || s.Apply == nil {
			t.Errorf("step %q needs both a Check and an Apply", s.Name)
		}
	}

	if all[0].Name != "repo-check" {
		t.Errorf("first step = %q, want repo-check", all[0].Name)
	}
	if last := all[len(all)-1].Name; last != "checkout-main" {
		t.Errorf("last step = %q, want checkout-main", last)
	}
}

// bash order: prepare, ssh key, github, data, baseline, finish.
func TestOrderIsTheGroupsInBashOrder(t *testing.T) {
	var want []steps.Step
	for _, g := range [][]steps.Step{steps.Prepare(), steps.SSHKey(), steps.GitHub(), steps.Data(), steps.Baseline(), steps.Finish()} {
		want = append(want, g...)
	}
	if got := names(steps.All()); !reflect.DeepEqual(got, names(want)) {
		t.Errorf("All() names =\n%v\nwant Prepare+SSHKey+GitHub+Data+Baseline+Finish =\n%v", got, names(want))
	}
}

func TestEveryGroupIsNonEmpty(t *testing.T) {
	groups := map[string][]steps.Step{
		"Prepare": steps.Prepare(), "SSHKey": steps.SSHKey(), "GitHub": steps.GitHub(),
		"Data": steps.Data(), "Baseline": steps.Baseline(), "Finish": steps.Finish(),
	}
	for name, g := range groups {
		if len(g) == 0 {
			t.Errorf("%s() returned no steps", name)
		}
	}
}

// Criterion 7: a second run against a converged machine applies nothing.
func TestAllTwiceAppliesNothing(t *testing.T) {
	fx := newFixture(t)
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("PATH", "/usr/bin")
	t.Cleanup(steps.SetNanobrewBin(fx.Path("absent-nb")))
	fx.Fake.Have("chezmoi", "gh", "rbw", "gitleaks", "mise", "yay")
	fx.Fake.On("sudo", "-n", "true").Return(run.Result{Code: 0})
	fx.Fake.On("chezmoi", chezmoiVerify(fx)...).Return(run.Result{Code: 0})
	fx.Env.Cfg = e2eConfig(t, fx.Home)
	fx.Env.Opts.CI = true // GitHub is never touched, so its steps are skipped
	fx.PutKey("PRIVATE\n", "ssh-ed25519 AAAA testhost\n")
	for _, d := range []string{"personal", "sidebiz"} {
		writeFile(t, fx.Path(d, ".envrc"), "x", 0o644)
	}
	if _, err := fx.Exec(steps.All()); err != nil {
		t.Fatalf("first run: %v\n%s", err, fx.Log)
	}
	rep, err := fx.Exec(steps.All())
	if err != nil {
		t.Fatalf("second run: %v\n%s", err, fx.Log)
	}
	if len(rep.Applied) != 0 {
		t.Errorf("second run applied %v, want nothing\n%s", rep.Applied, fx.Log)
	}
}
