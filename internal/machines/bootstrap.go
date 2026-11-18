package machines

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/morrieinmaas/athome/internal/config"
	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
)

// localtimePath is the laptop's timezone link; a var so tests can point it at a temp symlink.
var localtimePath = "/etc/localtime"

// laptopTimezone is the target of /etc/localtime after "zoneinfo/", "" when unknown.
func laptopTimezone() string {
	t, err := os.Readlink(localtimePath)
	if err != nil {
		return ""
	}
	_, tz, ok := strings.Cut(t, "zoneinfo/")
	if !ok {
		return ""
	}
	return tz
}

// tomlString is s as a TOML basic string: quotes, backslashes and control
// characters escaped, so no value can end its string or start another key.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
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
	b.WriteByte('"')
	return b.String()
}

// BootstrapTOML is the answers file a box's bootstrap reads, built from the
// laptop's chezmoi data. Keys with empty values are left out, and so is a
// timezone or locale that is not made of the characters those names use.
func BootstrapTOML(data map[string]any, m Machine, timezone, locale string) string {
	str := func(k string) string {
		v, ok := data[k]
		if !ok || v == nil {
			return ""
		}
		return fmt.Sprint(v)
	}
	type kv struct{ k, v string }
	list := []kv{
		{"machine", "remote"},
		{"scope", m.Scope},
		{"machineName", m.Name},
		{"githubHandle", str("githubHandle")},
		{"githubId", str("githubId")},
		{"personalName", str("personalName")},
	}
	switch m.Context() {
	case "work":
		list = append(list, kv{"workEmail", str("workEmail")}, kv{"workName", str("workName")})
	case "sidebiz":
		list = append(list, kv{"sidebizEmail", str("sidebizEmail")}, kv{"sidebizName", str("sidebizName")})
	}
	if !config.TimezoneRe.MatchString(timezone) {
		timezone = ""
	}
	if !config.LocaleRe.MatchString(locale) {
		locale = ""
	}
	list = append(list,
		kv{"includeAgents", str("includeAgents")},
		kv{"agentsRepo", str("agentsRepo")},
		kv{"meshProvider", str("meshProvider")},
		kv{"netbirdManagementUrl", str("netbirdManagementUrl")},
		kv{"generateSshKey", "false"},
		kv{"useBitwarden", "false"},
		kv{"timezone", timezone},
		kv{"locale", locale},
	)
	var b strings.Builder
	for _, e := range list {
		if e.v == "" {
			continue
		}
		fmt.Fprintf(&b, "%s = %s\n", e.k, tomlString(e.v))
	}
	return b.String()
}

func (d Deps) bootstrapTOML(m Machine) string {
	return BootstrapTOML(steps.StoredData(d.Home), m, laptopTimezone(), os.Getenv("LANG"))
}

const (
	pinnedVersionScript = `"$HOME/.local/bin/athome" version`
	readBootstrapScript = `cat "$HOME/.config/athome/bootstrap.toml"`
	uploadScript        = `umask 077; mkdir -p "$HOME/.config/athome" && cat > "$HOME/.config/athome/bootstrap.toml" && chmod 600 "$HOME/.config/athome/bootstrap.toml"`
	checkoutScript      = `d="$HOME/.local/share/athome"
u=https://github.com/morrieinmaas/athome
if [ -d "$d/.git" ]; then
  git -C "$d" remote set-url origin "$u" && git -C "$d" fetch --quiet --force --tags origin && git -C "$d" checkout --quiet --force --detach "refs/tags/$1"
else
  mkdir -p "$HOME/.local/share" && git clone --quiet --branch "$1" "$u" "$d"
fi`
	bootstrapScript   = `cd "$HOME/.local/share/athome" && ATHOME_VERSION="$1" ./scripts/bootstrap.sh --config "$HOME/.config/athome/bootstrap.toml"`
	bootstrapScriptNI = bootstrapScript + ` --non-interactive`
)

// execFn runs a script on a box without a terminal.
type execFn func(ctx context.Context, name, script, stdin string, args ...string) (run.Result, error)

// execNoTTY is Remote.Exec with ssh -T, for parallel runs that have no terminal.
func execNoTTY(r Remote) execFn {
	return func(ctx context.Context, name, script, stdin string, args ...string) (run.Result, error) {
		return r.Run.Run(ctx, run.Cmd{
			Name:  "ssh",
			Args:  r.sshArgs([]string{"-T", "-o", "BatchMode=yes"}, name, script, args),
			Stdin: stdin,
		})
	}
}

func okOrErr(what, name string, res run.Result, err error) error {
	if err != nil {
		return fmt.Errorf("%s on %s: %w", what, name, err)
	}
	if res.Code != 0 {
		return fmt.Errorf("%s on %s failed: %s", what, name, strings.TrimSpace(res.Stderr))
	}
	return nil
}

// bootstrapDone is step 5's check: the pinned athome is installed and the
// box's answers file equals the one generated now.
func bootstrapDone(ctx context.Context, d Deps, m Machine) (bool, error) {
	r := d.remote()
	res, err := r.Exec(ctx, m.Name, pinnedVersionScript, "")
	if err != nil {
		return false, err
	}
	if res.Code == 255 {
		return false, fmt.Errorf("ssh to %s failed: %s", m.Name, strings.TrimSpace(res.Stderr))
	}
	f := strings.Fields(res.Stdout)
	if res.Code != 0 || len(f) < 2 || strings.TrimPrefix(f[1], "v") != strings.TrimPrefix(m.Athome, "v") {
		return false, nil
	}
	res, err = r.Exec(ctx, m.Name, readBootstrapScript, "")
	if err != nil {
		return false, err
	}
	return res.Code == 0 && strings.TrimSpace(res.Stdout) == strings.TrimSpace(d.bootstrapTOML(m)), nil
}

// bootstrapBox uploads the answers, checks out the pinned tag and runs the
// box's bootstrap. interactive runs it with ssh -t so sudo can prompt; otherwise
// it runs with ssh -T and --non-interactive, its output going to out.
func bootstrapBox(ctx context.Context, d Deps, m Machine, interactive bool, out io.Writer) error {
	if tz := laptopTimezone(); tz != "" && !config.TimezoneRe.MatchString(tz) {
		d.say("note: timezone %q is not a zoneinfo name, so it is left out of the box's answers", tz)
	}
	if loc := os.Getenv("LANG"); loc != "" && !config.LocaleRe.MatchString(loc) {
		d.say("note: locale %q is not a locale name, so it is left out of the box's answers", loc)
	}
	r := d.remote()
	exec := execFn(r.Exec)
	if !interactive {
		exec = execNoTTY(r)
	}
	res, err := exec(ctx, m.Name, uploadScript, d.bootstrapTOML(m))
	if err := okOrErr("upload bootstrap.toml", m.Name, res, err); err != nil {
		return err
	}
	res, err = exec(ctx, m.Name, checkoutScript, "", m.Athome)
	if err := okOrErr("check out "+m.Athome, m.Name, res, err); err != nil {
		fmt.Fprint(out, res.Stdout+res.Stderr)
		return err
	}
	if interactive {
		res, err = r.Interactive(ctx, m.Name, bootstrapScript, m.Athome)
	} else {
		res, err = exec(ctx, m.Name, bootstrapScriptNI, "", m.Athome)
		fmt.Fprint(out, res.Stdout+res.Stderr)
	}
	if err != nil {
		return fmt.Errorf("bootstrap on %s: %w", m.Name, err)
	}
	if res.Code != 0 {
		return fmt.Errorf("bootstrap on %s exited %d", m.Name, res.Code)
	}
	return nil
}
