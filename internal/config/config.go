// Package config loads the non-interactive answers, porting scripts/bootstrap.sh B:169-234.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrUsage marks errors that exit with code 2.
var ErrUsage = errors.New("usage error")

type Options struct {
	ConfigPath string
	RepoRoot   string
	Home       string
	Env        map[string]string
	Machine    string
	MachineSet bool // --machine was given, even as "": it is then validated, not defaulted
	NoAgents   bool
}

type Config struct {
	Machine       string
	IncludeAgents bool
	Data          map[string]string
	Source        string
}

// answers is the bash table (B:189-209): toml key, env var, default.
var answers = []struct{ key, env, def string }{
	{"githubHandle", "ATHOME_GITHUB_HANDLE", ""},
	{"githubId", "ATHOME_GITHUB_ID", ""},
	{"personalName", "ATHOME_PERSONAL_NAME", ""},
	{"workEmail", "ATHOME_WORK_EMAIL", ""},
	{"workName", "ATHOME_WORK_NAME", ""},
	{"sidebizEmail", "ATHOME_SIDEBIZ_EMAIL", ""},
	{"sidebizName", "ATHOME_SIDEBIZ_NAME", ""},
	{"agentsRepo", "ATHOME_AGENTS_REPO", ""},
	{"slackDefaultWorkspace", "ATHOME_SLACK_WORKSPACE", ""},
	{"slackTeamId", "ATHOME_SLACK_TEAM_ID", ""},
	{"matterhornHost", "ATHOME_MATTERHORN_HOST", ""},
	{"matterhornUser", "ATHOME_MATTERHORN_USER", ""},
	{"netbirdManagementUrl", "ATHOME_NETBIRD_MGMT_URL", ""},
	{"nordvpnCountry", "ATHOME_NORDVPN_COUNTRY", ""},
	{"meshProvider", "ATHOME_MESH_PROVIDER", "meshnet"},
	{"extraPackages", "ATHOME_EXTRA_PACKAGES", ""},
	{"useBitwarden", "ATHOME_USE_BITWARDEN", ""},
	{"sshKeyAction", "ATHOME_SSH_KEY_ACTION", ""},
	{"generateSshKey", "ATHOME_GENERATE_SSH_KEY", ""},
	{"bitwardenUrl", "ATHOME_BW_BASE_URL", ""},
}

// Load resolves answers: env over file over default (B:186-234).
func Load(o Options) (Config, error) {
	cfg := Config{Machine: "personal", IncludeAgents: true, Data: map[string]string{}}

	var file []byte
	switch {
	case o.ConfigPath != "":
		b, err := os.ReadFile(o.ConfigPath)
		if err != nil {
			return cfg, fmt.Errorf("%w: --config: file not readable: %s", ErrUsage, o.ConfigPath)
		}
		file, cfg.Source = b, o.ConfigPath
	default:
		var cands []string
		if o.RepoRoot != "" {
			cands = append(cands, filepath.Join(o.RepoRoot, "bootstrap.local.toml"))
		}
		if o.Home != "" {
			cands = append(cands, filepath.Join(o.Home, ".config", "athome", "bootstrap.toml"))
		}
		for _, p := range cands {
			if b, err := os.ReadFile(p); err == nil {
				file, cfg.Source = b, p
				break
			}
		}
	}
	text := string(file)

	for _, a := range answers {
		v := o.Env[a.env]
		if v == "" {
			v = cfgGet(text, a.key)
		}
		if v == "" {
			v = a.def
		}
		cfg.Data[a.key] = v
	}

	if o.Machine != "" || o.MachineSet {
		cfg.Machine = o.Machine
	} else if m := cfgGet(text, "machine"); m != "" {
		cfg.Machine = m
	}
	if o.NoAgents {
		cfg.IncludeAgents = false
	} else if a := cfgGet(text, "includeAgents"); a != "" {
		switch a {
		case "true":
		case "false":
			cfg.IncludeAgents = false
		default:
			return cfg, fmt.Errorf("%w: includeAgents must be true|false (got: %s)", ErrUsage, a)
		}
	}
	if cfg.Machine != "personal" && cfg.Machine != "work" {
		return cfg, fmt.Errorf("%w: machine must be personal|work (got: %s)", ErrUsage, cfg.Machine)
	}
	return cfg, nil
}

// cfgGet ports cfg_get (B:182-185): first `key = value` line, trailing blanks
// trimmed, one pair of surrounding double quotes stripped, no inline comments.
func cfgGet(text, key string) string {
	re := regexp.MustCompile(`^[ \t\r\f\v]*` + regexp.QuoteMeta(key) + `[ \t\r\f\v]*=[ \t\r\f\v]*(.*)$`)
	for _, line := range strings.Split(text, "\n") {
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		v := strings.TrimRight(m[1], " \t\r\f\v")
		if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
			v = v[1 : len(v)-1]
		}
		return v
	}
	return ""
}
