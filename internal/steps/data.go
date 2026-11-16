package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/morrieinmaas/athome/internal/run"
)

// dataKeys are the string keys of chezmoi's [data], in the order bootstrap.sh
// writes them (B:880-898), between hostname/githubId and includeAgents.
var dataKeysAfterIdentity = []string{
	"personalName", "workEmail", "workName", "sidebizEmail", "sidebizName",
	"netbirdManagementUrl", "nordvpnCountry", "meshProvider", "bitwardenUrl",
	"agentsRepo", "extraPackages", "slackDefaultWorkspace", "slackTeamId",
	"matterhornHost", "matterhornUser",
}

type dataEntry struct {
	key  string
	val  string
	bool bool
	isB  bool
}

// Data seeds chezmoi's own config [data] before `chezmoi init`, so the first
// run needs no prompts. Ports B:816-900: derive the GitHub identity (handle from
// config, else `gh api user`; id from config, else gh, else the public API via
// curl), then write ~/.config/chezmoi/chezmoi.toml.
func Data() []Step {
	return []Step{{
		Name: "chezmoi-data",
		Check: func(ctx context.Context, env *Env) (bool, error) {
			entries, err := dataEntries(ctx, env)
			if err != nil {
				return false, err
			}
			var doc struct {
				Data map[string]any `toml:"data"`
			}
			if _, err := toml.DecodeFile(chezmoiConfigPath(env), &doc); err != nil {
				return false, nil // missing or unreadable: write it
			}
			// chezmoi init re-renders the file with more keys around the seed,
			// so only the seed keys count.
			for _, e := range entries {
				var want any = e.val
				if e.isB {
					want = e.bool
				}
				if got, ok := doc.Data[e.key]; !ok || got != want {
					return false, nil
				}
			}
			return true, nil
		},
		Apply: func(ctx context.Context, env *Env) error {
			entries, err := dataEntries(ctx, env)
			if err != nil {
				return err
			}
			var b strings.Builder
			b.WriteString("[data]\n")
			for _, e := range entries {
				if e.isB {
					fmt.Fprintf(&b, "    %-20s = %t\n", e.key, e.bool)
				} else {
					fmt.Fprintf(&b, "    %-20s = \"%s\"\n", e.key, tomlStr(e.val))
				}
			}
			path := chezmoiConfigPath(env)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			return os.WriteFile(path, []byte(b.String()), 0o600)
		},
	}}
}

// tomlStr escapes a value for a TOML basic string. bootstrap.sh escapes only
// backslash and quote (B:877); control characters are added so the file always parses.
func tomlStr(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\\' || r == '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func dataEntries(ctx context.Context, env *Env) ([]dataEntry, error) {
	d := env.Cfg.Data
	handle, id := dataIdentity(ctx, env)
	entries := []dataEntry{
		{key: "machineType", val: env.Cfg.Machine},
		{key: "hostname", val: shortHostname()}, // B:881; the key path uses dataHostname
		{key: "githubHandle", val: handle},
		{key: "githubId", val: id},
	}
	for _, k := range dataKeysAfterIdentity {
		v := d[k]
		if k == "meshProvider" && v == "" {
			v = "meshnet"
		}
		entries = append(entries, dataEntry{key: k, val: v})
	}
	entries = append(entries, dataEntry{key: "includeAgents", bool: env.Cfg.IncludeAgents, isB: true})
	return entries, nil
}

var idRe = regexp.MustCompile(`"id"\s*:\s*([0-9]*)`)

// dataIdentity ports B:830-846. Failures leave the value empty; it never errors.
func dataIdentity(ctx context.Context, env *Env) (handle, id string) {
	handle = env.Cfg.Data["githubHandle"]
	_, ghErr := env.Run.LookPath("gh")
	hasGh := ghErr == nil
	if handle == "" && hasGh {
		if r, err := env.Run.Run(ctx, dgCmd("gh", "api", "user", "--jq", ".login")); err == nil && r.Code == 0 {
			handle = strings.TrimSpace(r.Stdout)
		}
	}
	id = env.Cfg.Data["githubId"]
	if id == "" && handle != "" {
		if hasGh {
			if r, err := env.Run.Run(ctx, dgCmd("gh", "api", "users/"+handle, "--jq", ".id")); err == nil && r.Code == 0 {
				id = strings.TrimSpace(r.Stdout)
			}
		}
		if _, err := env.Run.LookPath("curl"); id == "" && err == nil {
			if r, err := env.Run.Run(ctx, dgCmd("curl", "-fsSL", "https://api.github.com/users/"+handle)); err == nil && r.Code == 0 {
				for _, line := range strings.Split(r.Stdout, "\n") {
					if m := idRe.FindStringSubmatch(line); m != nil {
						id = m[1]
						break
					}
				}
			}
		}
	}
	return handle, id
}

// dataHostname is chezmoi's hostname (B:352), falling back to the short hostname.
func dataHostname(ctx context.Context, env *Env) string {
	if r, err := env.Run.Run(ctx, dgCmd("chezmoi", "execute-template", "{{ .chezmoi.hostname }}")); err == nil && r.Code == 0 {
		if h := strings.TrimSpace(r.Stdout); h != "" {
			return h
		}
	}
	return shortHostname()
}

var (
	osHostname  = os.Hostname // gethostname(2), what bash's $HOSTNAME builtin is
	etcHostname = "/etc/hostname"
)

// shortHostname ports B:93-101: $HOSTNAME, then gethostname(2), then /etc/hostname.
func shortHostname() string {
	h := os.Getenv("HOSTNAME")
	if h == "" {
		h, _ = osHostname()
	}
	if h == "" {
		if b, err := os.ReadFile(etcHostname); err == nil {
			h = strings.TrimSpace(string(b))
		}
	}
	if i := strings.IndexByte(h, '.'); i >= 0 {
		h = h[:i]
	}
	if h == "" {
		return "machine"
	}
	return h
}

func dgCmd(name string, args ...string) run.Cmd { return run.Cmd{Name: name, Args: args} }
