package machines

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoginBlockSplitsTheJump(t *testing.T) {
	for jump, want := range map[string]string{
		"bastion":                  "ProxyCommand ssh -W [%h]:%p bastion\n",
		"ops@bastion":              "ProxyCommand ssh -W [%h]:%p -l ops bastion\n",
		"bastion:2222":             "ProxyCommand ssh -W [%h]:%p -p 2222 bastion\n",
		"ops@bastion.example:2222": "ProxyCommand ssh -W [%h]:%p -p 2222 -l ops bastion.example\n",
		"ops@10.0.0.1":             "ProxyCommand ssh -W [%h]:%p -l ops 10.0.0.1\n",
	} {
		m := gpu()
		m.Jump = jump
		if got := block(m, "", true); !strings.Contains(got, "    "+want) {
			t.Errorf("jump %q:\n%s", jump, got)
		}
		if got := block(m, "k", false); !strings.Contains(got, "    ProxyJump "+jump+"\n") {
			t.Errorf("jump %q, shared block:\n%s", jump, got)
		}
	}
}

func generatedConfig(home string) string {
	b, _ := os.ReadFile(filepath.Join(SSHDir(home), "config"))
	return string(b)
}

// Only the Include line is not enough: without ~/.ssh/athome/config ssh would
// resolve the bare name outside the pinned HostKeyAlias block.
func TestBoxCommandsWriteTheGeneratedConfigFirst(t *testing.T) {
	missing := func(t *testing.T) (*world, Deps) {
		w, d, _ := newWorld(t)
		if err := Save(d.Home, []Machine{acme()}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(SSHDir(d.Home), "config")); err == nil {
			t.Fatal("the generated config exists before the command")
		}
		return w, d
	}
	t.Run("sync", func(t *testing.T) {
		_, d := missing(t)
		if err := Sync(t.Context(), d, "acme-dev", false); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(generatedConfig(d.Home), "Host acme-dev") {
			t.Error("no Host block after sync")
		}
	})
	t.Run("clone", func(t *testing.T) {
		_, d := missing(t)
		if err := Clone(t.Context(), d, "acme/api", "acme-dev"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(generatedConfig(d.Home), "Host acme-dev") {
			t.Error("no Host block after clone")
		}
	})
	t.Run("keys inventory", func(t *testing.T) {
		w, d := missing(t)
		if _, err := KeysInventory(d.Home, w, "lap"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(generatedConfig(d.Home), "Host acme-dev") {
			t.Error("no Host block after KeysInventory")
		}
	})
	t.Run("remove", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("root ignores directory modes")
		}
		_, d := missing(t)
		// the inventory write fails, so the config the box steps wrote stays
		blocked := filepath.Join(d.Home, ".config", "athome")
		os.Chmod(blocked, 0o500)
		t.Cleanup(func() { os.Chmod(blocked, 0o700) })
		if err := Remove(t.Context(), d, "acme-dev", false); err == nil {
			t.Fatal("want the inventory step to fail")
		}
		if !strings.Contains(generatedConfig(d.Home), "Host acme-dev") {
			t.Error("no Host block when the box was reached")
		}
	})
}
