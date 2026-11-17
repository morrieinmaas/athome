package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/morrieinmaas/athome/internal/config"
	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
	"github.com/morrieinmaas/athome/internal/tui"
)

// storedChezmoiData is the [data] table of ~/.config/chezmoi/chezmoi.toml as strings.
func storedChezmoiData(home string) map[string]string {
	out := map[string]string{}
	for k, v := range steps.StoredData(home) {
		switch x := v.(type) {
		case string:
			out[k] = x
		case bool:
			out[k] = fmt.Sprint(x)
		}
	}
	return out
}

// buildSeed resolves each form field: precedence merge, then derived defaults.
func buildSeed(ctx context.Context, env *steps.Env, o initOpts, environ map[string]string) tui.Seed {
	vals, locked := seedPrecedence(storedChezmoiData(env.Home), env.Cfg, o, environ)
	seedDerivedDefaults(ctx, env, vals)
	return tui.Seed{
		Values:      vals,
		Locked:      locked,
		KeyExists:   keyExistsFor(ctx, env),
		CheckURL:    checkURL,
		CheckHandle: func(ctx context.Context, h string) error { return checkHandle(ctx, env.Run, h) },
	}
}

// seedPrecedence merges flag or env (locked), the answers file, and the stored
// chezmoi.toml. A value the file does not set never overrides a stored one.
func seedPrecedence(stored map[string]string, cfg config.Config, o initOpts, environ map[string]string) (map[string]string, map[string]bool) {
	vals := map[string]string{}
	locked := map[string]bool{}
	for k, v := range stored {
		vals[k] = v
	}
	fileSets := func(key string) bool { return cfg.Source != "" && config.FileValue(cfg.Source, key) != "" }
	for k, v := range cfg.Data {
		if v == "" {
			continue
		}
		if k == "meshProvider" && environ[config.DataEnv[k]] == "" && !fileSets(k) {
			continue // config's own default must not hide a stored choice
		}
		vals[k] = v
	}
	for k, e := range config.DataEnv {
		if environ[e] != "" {
			locked[k] = true
		}
	}
	// A stored id belongs to the stored handle; a different handle from env or
	// file drops it unless env or file also set the id.
	if vals["githubHandle"] != stored["githubHandle"] && environ[config.DataEnv["githubId"]] == "" && !fileSets("githubId") {
		delete(vals, "githubId")
	}
	if fileSets("machine") {
		vals["machineType"] = cfg.Machine
	}
	if fileSets("includeAgents") {
		vals["includeAgents"] = fmt.Sprint(cfg.IncludeAgents)
	}
	if o.MachineSet || o.Machine != "" {
		vals["machineType"], locked["machineType"] = cfg.Machine, true
	}
	if o.NoAgents {
		vals["includeAgents"], locked["includeAgents"] = "false", true
	}
	return vals, locked
}

// seedDerivedDefaults fills what no source gave: the gh login and the hostname.
func seedDerivedDefaults(ctx context.Context, env *steps.Env, vals map[string]string) {
	if vals["githubHandle"] == "" {
		if _, err := env.Run.LookPath("gh"); err == nil {
			ctx, cancel := context.WithTimeout(ctx, checkTimeout)
			defer cancel()
			if r, err := env.Run.Run(ctx, run.Cmd{Name: "gh", Args: []string{"api", "user", "--jq", ".login"}}); err == nil && r.Code == 0 {
				vals["githubHandle"] = strings.TrimSpace(r.Stdout)
			}
		}
	}
	if vals["hostname"] == "" {
		vals["hostname"] = steps.ShortHostname()
	}
}

// keyExistsFor answers for the hostname the form ends up with. mergeAnswers pins
// any hostname but the short one, so an explicit hostname names the key file
// directly; otherwise the steps' own key path (chezmoi's hostname) decides.
func keyExistsFor(ctx context.Context, env *steps.Env) func(string) bool {
	return func(hostname string) bool {
		path := filepath.Join(env.Home, ".ssh", hostname+"_ed25519")
		if hostname == "" || hostname == steps.ShortHostname() {
			path = steps.KeyPath(ctx, env)
		}
		_, err := os.Stat(path)
		return err == nil
	}
}
