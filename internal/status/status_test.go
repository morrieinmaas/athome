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
