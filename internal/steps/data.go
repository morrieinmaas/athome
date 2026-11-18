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

// dataKeys are the string keys of chezmoi's [data], in the order the bash bootstrap
// wrote them (B:880-898), between hostname/githubId and includeAgents.
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
	// Check computes the entries (one identity lookup) and Apply reuses them;
	// Apply clears the cache, and every run starts with Check, so a run never
	// sees the previous run's identity.
	var cached []dataEntry
	return []Step{{
		Name: "chezmoi-data",
		Check: func(ctx context.Context, env *Env) (bool, error) {
			entries, err := dataEntries(ctx, env)
			if err != nil {
				return false, err
			}
			cached = entries
			return dataDone(env, entries), nil
		},
		Apply: func(ctx context.Context, env *Env) error {
			entries := cached
			cached = nil
			if entries == nil {
				var err error
				if entries, err = dataEntries(ctx, env); err != nil {
					return err
				}
			}
			if dataPrompt(env, entries) {
				var err error
				if entries, err = dataEntries(ctx, env); err != nil {
					return err
				}
			}
			if dataStored(env, entries) {
				return nil // the prompts changed nothing: leave DataChanged false
			}
			return writeData(env, entries)
		},
	}}
}

// dataPrompt asks for the values the bash bootstrap prompts for (B:837, B:855-861) when
// env.Ask is set and the value is neither preset nor already stored. Answers go
// into env.Cfg.Data, where an env or config value would be. It reports whether
// anything was set, so the entries must be rebuilt. Only Apply calls it: a check
// never prompts.
func dataPrompt(env *Env, entries []dataEntry) bool {
	if env.Ask == nil {
		return false
	}
	if env.Cfg.Data == nil {
		env.Cfg.Data = map[string]string{}
	}
	changed := false
	for _, e := range entries {
		if e.key == "githubHandle" && e.val == "" { // B:837, EOF leaves it empty
			if h := env.Ask.String("GitHub username: ", true); h != "" {
				env.Cfg.Data["githubHandle"] = h
				changed = true
			}
		}
	}
	d := env.Cfg.Data
	if bitwardenURLUnasked(env) { // B:855-861
		env.Ask.Say("Bitwarden server (for rbw secrets):\n")
		env.Ask.Say("  empty  = US cloud (bitwarden.com)\n")
		env.Ask.Say("  EU     = https://vault.bitwarden.eu\n")
		env.Ask.Say("  self-hosted Vaultwarden = your URL (https://vault.example.com)\n")
		if u := env.Ask.String("Bitwarden server URL [empty = US cloud]: ", false); u != "" {
			d["bitwardenUrl"] = u
			changed = true
		}
	}
	return changed
}

// bitwardenURLUnasked: no URL in the config or chezmoi.toml, and Bitwarden is
// not switched off, so an interactive run asks for it.
func bitwardenURLUnasked(env *Env) bool {
	return env.Cfg.Machine != "remote" && !env.Answered["bitwardenUrl"] && env.Cfg.Data["bitwardenUrl"] == "" && env.Cfg.Data["useBitwarden"] != "false" && storedBitwardenURL(env) == ""
}

func storedBitwardenURL(env *Env) string {
	s, _ := storedData(env)["bitwardenUrl"].(string)
	return s
}

// dataDone: chezmoi.toml already holds every seed key with the wanted value.
func dataDone(env *Env, entries []dataEntry) bool {
	for _, e := range entries {
		if env.Ask != nil && e.key == "githubHandle" && e.val == "" {
			return false // asked on every interactive run until answered
		}
	}
	if env.Ask != nil && bitwardenURLUnasked(env) {
		return false // likewise the Bitwarden URL
	}
	return dataStored(env, entries)
}

// dataStored: chezmoi.toml holds every seed key with the wanted value.
func dataStored(env *Env, entries []dataEntry) bool {
	var doc struct {
		Data map[string]any `toml:"data"`
	}
	if _, err := toml.DecodeFile(chezmoiConfigPath(env), &doc); err != nil {
		return false // missing or unreadable: write it
	}
	// chezmoi init re-renders the file with more keys around the seed,
	// so only the seed keys count.
	for _, e := range entries {
		var want any = e.val
		if e.isB {
			want = e.bool
		}
		if got, ok := doc.Data[e.key]; !ok || got != want {
			return false
		}
	}
	return true
}

// writeData writes the seeded [data] table to chezmoi.toml (B:875-900).
func writeData(env *Env, entries []dataEntry) error {
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
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return err
	}
	env.DataChanged = true
	return nil
}

// tomlStr escapes a value for a TOML basic string. The bash bootstrap escaped only
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
		{key: "hostname", val: dataShortHostname(env)}, // B:881; the key path uses dataHostname
		{key: "githubHandle", val: handle},
		{key: "githubId", val: id},
	}
	for _, k := range dataKeysAfterIdentity {
		v := d[k]
		if k == "meshProvider" && v == "" {
			v = "meshnet"
		}
		if k == "bitwardenUrl" && v == "" && !env.Answered["bitwardenUrl"] {
			v = storedBitwardenURL(env) // an earlier answer is kept unless the form answered it
		}
		entries = append(entries, dataEntry{key: k, val: v})
	}
	if env.Cfg.Machine == "remote" {
		for _, k := range []string{"scope", "machineName", "timezone", "locale"} {
			entries = append(entries, dataEntry{key: k, val: d[k]})
		}
	}
	entries = append(entries, dataEntry{key: "includeAgents", bool: env.Cfg.IncludeAgents, isB: true})
	return entries, nil
}

var idRe = regexp.MustCompile(`"id"\s*:\s*([0-9]*)`)

// StoredData decodes the [data] table of the chezmoi.toml under home; nil when it is missing or unreadable.
func StoredData(home string) map[string]any {
	var doc struct {
		Data map[string]any `toml:"data"`
	}
	if _, err := toml.DecodeFile(filepath.Join(home, ".config", "chezmoi", "chezmoi.toml"), &doc); err != nil {
		return nil
	}
	return doc.Data
}

// storedData is the [data] table already in chezmoi.toml, or nil.
func storedData(env *Env) map[string]any { return StoredData(env.Home) }

// dataIdentity ports B:830-846. Failures leave the value empty; it never errors.
// A value that cannot be derived (offline rerun) falls back to the one already
// stored, so a stored identity is never replaced by "". The stored id is only
// reused for the stored handle; for another handle it would be someone else's.
func dataIdentity(ctx context.Context, env *Env) (handle, id string) {
	stored := storedData(env)
	storedStr := func(k string) string { s, _ := stored[k].(string); return s }
	handle = env.Cfg.Data["githubHandle"]
	_, ghErr := env.Run.LookPath("gh")
	hasGh := ghErr == nil
	if handle == "" && hasGh {
		if r, err := env.Run.Run(ctx, dgCmd("gh", "api", "user", "--jq", ".login")); err == nil && r.Code == 0 {
			handle = strings.TrimSpace(r.Stdout)
		}
	}
	if handle == "" {
		handle = storedStr("githubHandle")
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
	if id == "" && handle == storedStr("githubHandle") {
		id = storedStr("githubId")
	}
	return handle, id
}

// dataShortHostname is the [data].hostname to write: the form's answer for this
// run, else the short hostname as build 2 wrote it every run (B:881).
func dataShortHostname(env *Env) string {
	if h := env.Cfg.Data["hostname"]; h != "" {
		return h
	}
	return shortHostname()
}

// ShortHostname is the hostname the data step records when none is answered.
func ShortHostname() string { return shortHostname() }

// KeyPath is where the machine's ssh key lives, for callers outside the steps.
func KeyPath(ctx context.Context, env *Env) string { return skKeyPath(ctx, env) }

// dataHostname is chezmoi's hostname (B:352), falling back to the short hostname.
func dataHostname(ctx context.Context, env *Env) string {
	if r, err := env.Run.Run(ctx, dgCmd("chezmoi", "execute-template", "{{ .chezmoi.hostname }}")); err == nil && r.Code == 0 {
		if h := strings.TrimSpace(r.Stdout); h != "" {
			return h
		}
	}
	return shortHostname()
}

// KeyHostname is the hostname init names the per-machine ssh key from. chezmoi's
// .chezmoi.hostname is the short os.Hostname(), so no external command is needed.
func KeyHostname() string { return shortHostname() }

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
