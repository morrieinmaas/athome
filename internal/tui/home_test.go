package tui_test

import (
	"io"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/tui"
)

func TestHomeSetUp(t *testing.T) {
	var out syncBuf
	got, err := tui.RunHome(strings.NewReader("\r"), &out, tui.HomeInfo{
		Version: "v1.2.3", Commit: "abc123", Repo: "git@x:me/dots.git",
		MachineType: "personal", Hostname: "box", Handle: "octo", SetUp: true,
	})
	if err != nil || got != "init" {
		t.Fatalf("got %q, %v", got, err)
	}
	for _, w := range []string{"v1.2.3", "abc123", "git@x:me/dots.git", "personal", "box", "octo"} {
		if !strings.Contains(out.String(), w) {
			t.Errorf("missing %q in %q", w, out.String())
		}
	}
	if strings.Contains(out.String(), "not set up") {
		t.Error("unexpected not set up")
	}
}

func TestHomeNotSetUpQuit(t *testing.T) {
	var out syncBuf
	got, err := tui.RunHome(io.MultiReader(strings.NewReader("j\r")), &out, tui.HomeInfo{Version: "v1", Commit: "c", Repo: "r"})
	if err != nil || got != "quit" {
		t.Fatalf("got %q, %v", got, err)
	}
	if !strings.Contains(out.String(), "not set up") {
		t.Errorf("want not set up in %q", out.String())
	}
}
