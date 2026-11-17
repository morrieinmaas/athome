package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
	"github.com/morrieinmaas/athome/internal/tui"
)

// Seams for tests: the screens and the terminal check.
var (
	stdoutIsTTY = func() bool { return isTerminal(os.Stdout) }
	runForm     = tui.RunForm
	runProgress = tui.RunProgress
	runHome     = tui.RunHome
)

const checkTimeout = 5 * time.Second

// useTUI: init shows the form when it would prompt, unless --plain or ATHOME_PLAIN asks for lines.
func useTUI(o initOpts, environ map[string]string) bool {
	if o.Plain || o.Config != "" { // a --config run keeps its line prompts, never the form
		return false
	}
	if environ["ATHOME_PLAIN"] != "" && environ["ATHOME_PLAIN"] != "0" {
		return false
	}
	return isInteractive(o.NonInter || o.Yes, stdinIsTTY()) && stdoutIsTTY()
}

// dataEnv maps the form's data keys to the ATHOME_* variable that presets (and locks) them.
var dataEnv = map[string]string{
	"githubHandle": "ATHOME_GITHUB_HANDLE", "githubId": "ATHOME_GITHUB_ID",
	"personalName": "ATHOME_PERSONAL_NAME", "workEmail": "ATHOME_WORK_EMAIL", "workName": "ATHOME_WORK_NAME",
	"sidebizEmail": "ATHOME_SIDEBIZ_EMAIL", "sidebizName": "ATHOME_SIDEBIZ_NAME",
	"agentsRepo": "ATHOME_AGENTS_REPO", "slackDefaultWorkspace": "ATHOME_SLACK_WORKSPACE",
	"slackTeamId": "ATHOME_SLACK_TEAM_ID", "matterhornHost": "ATHOME_MATTERHORN_HOST",
	"matterhornUser": "ATHOME_MATTERHORN_USER", "netbirdManagementUrl": "ATHOME_NETBIRD_MGMT_URL",
	"nordvpnCountry": "ATHOME_NORDVPN_COUNTRY", "meshProvider": "ATHOME_MESH_PROVIDER",
	"extraPackages": "ATHOME_EXTRA_PACKAGES", "useBitwarden": "ATHOME_USE_BITWARDEN",
	"sshKeyAction": "ATHOME_SSH_KEY_ACTION", "generateSshKey": "ATHOME_GENERATE_SSH_KEY",
	"bitwardenUrl": "ATHOME_BW_BASE_URL",
}

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

// buildSeed resolves each form field: flag or env (locked), config file, stored chezmoi.toml, default.
func buildSeed(ctx context.Context, env *steps.Env, o initOpts, environ map[string]string) tui.Seed {
	stored := storedChezmoiData(env.Home)
	vals := map[string]string{}
	locked := map[string]bool{}
	for k, v := range stored {
		vals[k] = v
	}
	for k, v := range env.Cfg.Data {
		if v == "" {
			continue
		}
		if k == "meshProvider" && v == "meshnet" && environ[dataEnv[k]] == "" && env.Cfg.Source == "" {
			continue // config's own default must not hide a stored choice
		}
		vals[k] = v
	}
	for k, e := range dataEnv {
		if environ[e] != "" {
			locked[k] = true
		}
	}
	if env.Cfg.Source != "" {
		vals["machineType"] = env.Cfg.Machine
		vals["includeAgents"] = fmt.Sprint(env.Cfg.IncludeAgents)
	}
	if o.MachineSet || o.Machine != "" {
		vals["machineType"], locked["machineType"] = env.Cfg.Machine, true
	}
	if o.NoAgents {
		vals["includeAgents"], locked["includeAgents"] = "false", true
	}
	if vals["githubHandle"] == "" {
		if _, err := env.Run.LookPath("gh"); err == nil {
			if r, err := env.Run.Run(ctx, run.Cmd{Name: "gh", Args: []string{"api", "user", "--jq", ".login"}}); err == nil && r.Code == 0 {
				vals["githubHandle"] = strings.TrimSpace(r.Stdout)
			}
		}
	}
	if vals["hostname"] == "" {
		vals["hostname"] = steps.ShortHostname()
	}
	_, statErr := os.Stat(steps.KeyPath(ctx, env))
	return tui.Seed{
		Values:      vals,
		Locked:      locked,
		KeyExists:   statErr == nil,
		CheckURL:    checkURL,
		CheckHandle: func(ctx context.Context, h string) error { return checkHandle(ctx, env.Run, h) },
	}
}

var httpGet = func(ctx context.Context, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

// checkURL: an https URL that answers a GET within 5 s.
func checkURL(ctx context.Context, u string) error {
	if !strings.HasPrefix(u, "https://") {
		return errors.New("the URL must start with https://")
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	if _, err := httpGet(ctx, u); err != nil {
		return fmt.Errorf("%s did not answer: %v", u, err)
	}
	return nil
}

var handleRe = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$`)

// checkHandle: the GitHub user exists, by `gh api users/<h>` else the public API.
func checkHandle(ctx context.Context, r run.Runner, h string) error {
	if !handleRe.MatchString(h) {
		return fmt.Errorf("%q is not a valid GitHub username", h)
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	if _, err := r.LookPath("gh"); err == nil {
		if res, err := r.Run(ctx, run.Cmd{Name: "gh", Args: []string{"api", "users/" + h}}); err == nil && res.Code == 0 {
			return nil
		}
	}
	code, err := httpGet(ctx, "https://api.github.com/users/"+h)
	switch {
	case err != nil:
		return fmt.Errorf("could not check %s: %v", h, err)
	case code == http.StatusNotFound:
		return fmt.Errorf("no GitHub user %q", h)
	case code != http.StatusOK:
		return fmt.Errorf("could not check %s: GitHub answered %d", h, code)
	}
	return nil
}

// mergeAnswers puts the form's answers where config keeps them.
func mergeAnswers(env *steps.Env, ans map[string]string) {
	if env.Cfg.Data == nil {
		env.Cfg.Data = map[string]string{}
	}
	env.Answered = map[string]bool{}
	for k := range ans {
		env.Answered[k] = true // an empty answer is still an answer: the prompts must not ask again
	}
	for k, v := range ans {
		switch k {
		case "machineType":
			env.Cfg.Machine = v
		case "includeAgents":
			env.Cfg.IncludeAgents = v != "false"
		case "hostname":
			if v != "" && v != steps.ShortHostname() {
				env.Cfg.Data["hostname"] = v // the data step and the key path honour an explicit hostname
			}
		default:
			env.Cfg.Data[k] = v
		}
	}
	for _, k := range []string{"sshKeyAction", "generateSshKey"} {
		env.Cfg.Data[k] = ans[k] // the form answers one of them; the other must not linger
	}
}

// runInitTUI shows the form, then the progress view over the same step list.
func runInitTUI(env *steps.Env, o initOpts, stdout, stderr io.Writer) int {
	defer env.RunCleanup()
	ctx, stop := notifyContext(context.Background(), stopSignals...)
	defer stop()

	ans, err := runForm(ctx, os.Stdin, os.Stdout, buildSeed(ctx, env, o, environMap()))
	if errors.Is(err, tui.ErrQuit) {
		return exitInterrupted
	}
	if err != nil {
		fmt.Fprintf(stderr, "athome init: %v\n", err)
		return exitError
	}
	mergeAnswers(env, ans)

	bindCancel(ctx, env, stderr)
	if err := steps.Setup(ctx, env); err != nil {
		fmt.Fprintf(stderr, "athome init: %v\n", err)
		return exitError
	}
	inner := env.Run
	var stopSudo func()
	defer func() {
		if stopSudo != nil {
			stopSudo()
		}
		env.Run = inner
	}()
	wire := func(around func(func())) {
		if env.Ask != nil {
			env.Ask.Around = around
		}
		var warm run.Runner
		warm, stopSudo = steps.WarmSudo(run.Suspending(inner, around), env.Log)
		env.Run = warm
	}
	rep, err := runProgress(ctx, os.Stdin, os.Stdout, env, stepList(), wire)
	if errors.Is(err, context.Canceled) {
		return exitInterrupted
	}
	if err != nil {
		fmt.Fprintf(stderr, "athome init: %v\n", err)
		return exitError
	}
	printFinish(stdout, rep, env.Warnings, steps.FollowupLogPath())
	return exitOK
}

// runHomeScreen is `athome` on a terminal: the home screen, then init when chosen.
func runHomeScreen(stdout, stderr io.Writer) int {
	info := tui.HomeInfo{Version: Version, Commit: Commit}
	if home, err := os.UserHomeDir(); err == nil {
		info.Repo, _ = findRepoRoot(home)
		d := storedChezmoiData(home)
		info.MachineType, info.Hostname, info.Handle = d["machineType"], d["hostname"], d["githubHandle"]
		info.SetUp = info.MachineType != ""
	}
	choice, err := runHome(os.Stdin, os.Stdout, info)
	if err != nil {
		fmt.Fprintf(stderr, "athome: %v\n", err)
		return exitError
	}
	if choice == "init" {
		return runInit(nil, stdout, stderr)
	}
	return exitOK
}
