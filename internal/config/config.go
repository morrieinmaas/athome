// Package config loads the non-interactive answers, porting scripts/bootstrap-legacy.sh B:169-234.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
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
	{"scope", "ATHOME_SCOPE", ""},
	{"machineName", "ATHOME_MACHINE_NAME", ""},
	{"timezone", "ATHOME_TIMEZONE", ""},
	{"locale", "ATHOME_LOCALE", ""},
}

// DataEnv maps each answer key to the ATHOME_* variable that presets it.
var DataEnv = func() map[string]string {
	m := make(map[string]string, len(answers))
	for _, a := range answers {
		m[a.key] = a.env
	}
	return m
}()

// FileValue is the value of key in the answers file at path, or "" when the file or the key is missing.
func FileValue(path, key string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return cfgGet(string(b), key)
}

// Load resolves answers: env over file over default (B:186-234).
func Load(o Options) (Config, error) {
	cfg := Config{Machine: "personal", IncludeAgents: true, Data: map[string]string{}}

	file, source, err := readAnswersFile(o)
	if err != nil {
		return cfg, err
	}
	cfg.Source = source
	text := string(file)

	for _, a := range answers {
		v := o.Env[a.env]
		if v == "" {
			v = cfgGet(text, a.key)
		}
		if v == "" {
			v = a.def
		}
		if !utf8.ValidString(v) { // chezmoi.toml is TOML, which must be UTF-8
			return cfg, fmt.Errorf("%w: %s is not valid UTF-8 (set via %s or the config file)", ErrUsage, a.key, a.env)
		}
		cfg.Data[a.key] = v
	}

	machine, includeAgents, err := resolveMachineAgents(o, text)
	if err != nil {
		return cfg, err
	}
	cfg.Machine, cfg.IncludeAgents = machine, includeAgents
	if machine == "remote" {
		if err := validateRemote(cfg.Data); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

// resolveMachineAgents picks machine and includeAgents: flag over file over
// default, then validates both (B:222-234).
func resolveMachineAgents(o Options, text string) (machine string, includeAgents bool, err error) {
	machine, includeAgents = "personal", true
	if o.Machine != "" || o.MachineSet {
		machine = o.Machine
	} else if m := cfgGet(text, "machine"); m != "" {
		machine = m
	}
	if o.NoAgents {
		includeAgents = false
	} else if a := cfgGet(text, "includeAgents"); a != "" {
		switch a {
		case "true":
		case "false":
			includeAgents = false
		default:
			return machine, includeAgents, fmt.Errorf("%w: includeAgents must be true|false (got: %s)", ErrUsage, a)
		}
	}
	if machine != "personal" && machine != "work" && machine != "remote" {
		return machine, includeAgents, fmt.Errorf("%w: machine must be personal|work|remote (got: %s)", ErrUsage, machine)
	}
	return machine, includeAgents, nil
}

// The patterns a remote machine's answers and the machine inventory share.
var (
	ScopeRe       = regexp.MustCompile(`^(work|personal|sidebiz)/[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)
	MachineNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
	TimezoneRe    = regexp.MustCompile(`^[A-Za-z0-9_+./-]+$`)
	LocaleRe      = regexp.MustCompile(`^[A-Za-z0-9_.@-]+$`)
)

// validateRemote checks the answers a remote box needs; the same rules the
// machine inventory applies, so a value is safe in a path and a shell word.
func validateRemote(d map[string]string) error {
	scope := d["scope"]
	ok := ScopeRe.MatchString(scope)
	for _, seg := range strings.Split(scope, "/") {
		if seg == "." || seg == ".." {
			ok = false
		}
	}
	if !ok {
		return fmt.Errorf("%w: scope must be work|personal|sidebiz/<path> for a remote machine (got: %q; set ATHOME_SCOPE or scope in the config file)", ErrUsage, scope)
	}
	if !MachineNameRe.MatchString(d["machineName"]) || d["machineName"] == "local" || d["machineName"] == "0" {
		return fmt.Errorf("%w: machineName must match %s and not be local or 0 for a remote machine (got: %q; set ATHOME_MACHINE_NAME or machineName in the config file)", ErrUsage, MachineNameRe, d["machineName"])
	}
	if tz := d["timezone"]; tz != "" && !TimezoneRe.MatchString(tz) {
		return fmt.Errorf("%w: timezone must match %s (got: %q)", ErrUsage, TimezoneRe, tz)
	}
	if loc := d["locale"]; loc != "" && !LocaleRe.MatchString(loc) {
		return fmt.Errorf("%w: locale must match %s (got: %q)", ErrUsage, LocaleRe, loc)
	}
	return nil
}

// readAnswersFile finds the answers file: --config, else the repo's
// bootstrap.local.toml, else ~/.config/athome/bootstrap.toml (B:172-181).
func readAnswersFile(o Options) (content []byte, source string, err error) {
	if o.ConfigPath != "" {
		b, err := os.ReadFile(o.ConfigPath)
		if err != nil {
			return nil, "", fmt.Errorf("%w: --config: file not readable: %s", ErrUsage, o.ConfigPath)
		}
		return b, o.ConfigPath, nil
	}
	var cands []string
	if o.RepoRoot != "" {
		cands = append(cands, filepath.Join(o.RepoRoot, "bootstrap.local.toml"))
	}
	if o.Home != "" {
		cands = append(cands, filepath.Join(o.Home, ".config", "athome", "bootstrap.toml"))
	}
	for _, p := range cands {
		if b, err := os.ReadFile(p); err == nil {
			return b, p, nil
		}
	}
	return nil, "", nil
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
