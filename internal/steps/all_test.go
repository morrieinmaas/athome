package steps_test

import (
	"reflect"
	"testing"

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
