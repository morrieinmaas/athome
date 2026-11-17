package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	if plain := strings.ToLower(environ["ATHOME_PLAIN"]); plain == "1" || plain == "true" {
		return false
	}
	return isInteractive(o.NonInter || o.Yes, stdinIsTTY()) && stdoutIsTTY()
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

// checkURL: an https URL that answers a GET within 5 s. Only a transport
// failure is overridable (tui.ErrCheckUnavailable); a malformed URL is not.
func checkURL(ctx context.Context, u string) error {
	if !strings.HasPrefix(u, "https://") {
		return errors.New("the URL must start with https://")
	}
	if pu, err := url.Parse(u); err != nil || pu.Host == "" {
		return fmt.Errorf("%q is not a valid URL", u)
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	if _, err := httpGet(ctx, u); err != nil {
		return fmt.Errorf("%w %s: it did not answer: %v", tui.ErrCheckUnavailable, u, err)
	}
	return nil
}

// A GitHub username is alphanumeric with single hyphens inside, at most 39 long.
var handleRe = regexp.MustCompile(`^[A-Za-z0-9]+(?:-[A-Za-z0-9]+)*$`)

// checkHandle: the GitHub user exists, by `gh api users/<h>` else the public API.
// A malformed handle and a 404 block; a transport error or an unexpected status
// wraps tui.ErrCheckUnavailable so an offline machine can still go on.
func checkHandle(ctx context.Context, r run.Runner, h string) error {
	if len(h) > 39 || !handleRe.MatchString(h) {
		return fmt.Errorf("%q is not a valid GitHub username", h)
	}
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	path := "users/" + url.PathEscape(h)
	if _, err := r.LookPath("gh"); err == nil {
		if res, err := r.Run(ctx, run.Cmd{Name: "gh", Args: []string{"api", path}}); err == nil && res.Code == 0 {
			return nil
		}
	}
	code, err := httpGet(ctx, "https://api.github.com/"+path)
	switch {
	case err != nil:
		return fmt.Errorf("%w %s: %v", tui.ErrCheckUnavailable, h, err)
	case code == http.StatusNotFound:
		return fmt.Errorf("no GitHub user %q", h)
	case code != http.StatusOK:
		return fmt.Errorf("%w %s: GitHub answered %d", tui.ErrCheckUnavailable, h, code)
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
			if v == "" {
				v = steps.ShortHostname() // the form's empty hostname means this machine's name
			}
			env.Cfg.Data["hostname"] = v // the data step records it for this run only
		default:
			env.Cfg.Data[k] = v
		}
	}
	for _, k := range []string{"sshKeyAction", "generateSshKey"} {
		env.Cfg.Data[k] = ans[k] // the form answers one of them; the other must not linger
	}
	if env.Cfg.Data["sshKeyAction"] == "" && importsSSHKey(env.Opts) {
		env.Cfg.Data["sshKeyAction"] = "use" // the form saw no key, but the import creates one
	}
}

func importsSSHKey(o steps.Opts) bool {
	return o.ImportSSHBW != "" || (o.ImportFrom != "" && o.ImportSSH)
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
