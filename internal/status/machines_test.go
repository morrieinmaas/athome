package status_test

import (
	"errors"
	"testing"

	"github.com/morrieinmaas/athome/internal/status"
)

func TestMachinesFrom(t *testing.T) {
	if s := status.MachinesFrom(0, nil, nil); s.Count != 0 || s.Warnings == nil || len(s.Warnings) != 0 {
		t.Errorf("empty: %+v", s)
	}
	if s := status.MachinesFrom(2, []string{"w"}, nil); s.Count != 2 || len(s.Warnings) != 1 || s.Warnings[0] != "w" {
		t.Errorf("warned: %+v", s)
	}
	s := status.MachinesFrom(3, []string{"w"}, errors.New("machines.acme-dev: scope bad"))
	if s.Count != 0 || len(s.Warnings) != 1 || s.Warnings[0] != "machines.acme-dev: scope bad" {
		t.Errorf("broken: %+v", s)
	}
}
