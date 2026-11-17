package status_test

import (
	"context"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/run/runtest"
	"github.com/morrieinmaas/athome/internal/status"
)

func collect(t *testing.T, version, lsRemote string) status.Report {
	t.Helper()
	repo, home := t.TempDir(), t.TempDir()
	f := runtest.NewFake()
	f.On("git", "-C", repo, "ls-remote", "--tags", "--refs", "origin").Return(run.Result{Stdout: lsRemote})
	return status.Collect(context.Background(), f, version, "abc", home, repo)
}

func TestAthomeUsesSelfupdateVersionRules(t *testing.T) {
	// v1.0.00 has a leading zero: selfupdate.Parse rejects it, so it is not a release.
	r := collect(t, "0.5.2", "a\trefs/tags/v0.5.2\nb\trefs/tags/v1.0.00\nc\trefs/tags/v0.5.10-rc\n")
	c := r.Components[0]
	if c.Wanted != "v0.5.2" || c.Status != status.Current {
		t.Errorf("athome = %+v", c)
	}
}

func TestAthomeBehind(t *testing.T) {
	c := collect(t, "0.5.1", "a\trefs/tags/v0.5.2\nb\trefs/tags/v0.5.10\n").Components[0]
	if c.Wanted != "v0.5.10" || c.Status != status.NotCurrent {
		t.Errorf("athome = %+v", c)
	}
}

func TestAthomeDevBuildAndNoTags(t *testing.T) {
	if c := collect(t, "dev", "a\trefs/tags/v0.5.2\n").Components[0]; c.Status != status.Unknown || c.Wanted != "v0.5.2" {
		t.Errorf("dev = %+v", c)
	}
	if c := collect(t, "0.5.2", "").Components[0]; c.Status != status.Unknown || c.Wanted != "" {
		t.Errorf("no tags = %+v", c)
	}
}

func miseComponent(t *testing.T, stdout string) (status.Component, run.Cmd, string) {
	t.Helper()
	repo, home := t.TempDir(), t.TempDir()
	f := runtest.NewFake()
	f.Have("mise")
	f.On("mise", "ls", "--missing", "--json").Return(run.Result{Stdout: stdout})
	rep := status.Collect(context.Background(), f, "dev", "abc", home, repo)
	for _, c := range f.Calls() {
		if c.Name == "mise" {
			return rep.Components[2], c, home
		}
	}
	t.Fatal("mise was not run")
	return status.Component{}, run.Cmd{}, ""
}

func TestMiseRunsInHome(t *testing.T) {
	_, cmd, home := miseComponent(t, "{}")
	if cmd.Dir != home {
		t.Errorf("mise ran in %q, want %q", cmd.Dir, home)
	}
}

func TestMiseCounts(t *testing.T) {
	if c, _, _ := miseComponent(t, "{}"); c.Status != status.Current {
		t.Errorf("empty = %+v", c)
	}
	if c, _, _ := miseComponent(t, `{"node": [{}]}`); c.Status != status.NotCurrent {
		t.Errorf("missing = %+v", c)
	}
	if c, _, _ := miseComponent(t, "not json"); c.Status != status.Unknown {
		t.Errorf("bad json = %+v", c)
	}
}

func TestMissingCount(t *testing.T) {
	for in, want := range map[string]int{"": 0, " \n": 0, "{}": 0, "[]": 0, `{"a":1,"b":2}`: 2, `[1]`: 1} {
		if n, err := status.MissingCount(in); err != nil || n != want {
			t.Errorf("MissingCount(%q) = %d, %v; want %d", in, n, err, want)
		}
	}
	for _, in := range []string{"nope", "null", "3", `"x"`} {
		if _, err := status.MissingCount(in); err == nil {
			t.Errorf("MissingCount(%q) must fail", in)
		}
	}
}
