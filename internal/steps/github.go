package steps

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type ghMode int

const (
	ghSkip       ghMode = iota // nothing to do (CI, no gh, not authed, no key)
	ghRegistered               // an auth key is already on GitHub; only the signing key may need uploading
	ghUpload                   // upload auth and signing keys, flip protocol, seed ssh config, flip remote
)

type ghPlan struct {
	mode    ghMode
	pub     string // per-host public key
	sshHost string
	host    string // short hostname, used in key titles
	signPub string
}

const (
	scopePublic  = "admin:public_key"
	scopeSigning = "admin:ssh_signing_key"
)

// GitHub ports B:498-756: upload the per-host ssh key (auth and signing) to
// GitHub, seed ~/.ssh/config and switch the repo remote to ssh. It never fails
// the run on a GitHub problem; it logs what to do by hand instead.
func GitHub() []Step {
	var plan *ghPlan // decided once in Check, reused by Apply
	decide := func(ctx context.Context, env *Env) ghPlan {
		if plan == nil {
			p := ghDecide(ctx, env)
			plan = &p
		}
		return *plan
	}
	return []Step{{
		Name: "github-keys",
		Check: func(ctx context.Context, env *Env) (bool, error) {
			p := decide(ctx, env)
			switch p.mode {
			case ghSkip:
				return true, nil
			case ghRegistered:
				return ghSigningState(ctx, env, p) == sigDone, nil
			}
			return false, nil
		},
		Apply: func(ctx context.Context, env *Env) error {
			p := decide(ctx, env)
			switch p.mode {
			case ghRegistered:
				ghEnsureSigning(ctx, env, p)
			case ghUpload:
				ghUploadAll(ctx, env, p)
			}
			return nil
		},
	}}
}

func ghDecide(ctx context.Context, env *Env) ghPlan {
	var p ghPlan
	if env.Opts.CI {
		envLogf(env, "==> ATHOME_CI set, skipping GitHub SSH-key upload (test/headless; never touches a real account)\n")
		return p
	}
	p.sshHost = dataHostname(ctx, env)
	p.pub = filepath.Join(env.Home, ".ssh", p.sshHost+"_ed25519.pub")
	if _, err := os.Stat(p.pub); err != nil {
		envLogf(env, "no %s, skipping ssh-key upload\n", p.pub)
		return p
	}
	if _, err := env.Run.LookPath("gh"); err != nil {
		envLogf(env, "gh not installed, skipping ssh-key upload (run `gh ssh-key add` manually later)\n")
		return p
	}
	if r, err := env.Run.Run(ctx, dgCmd("gh", "auth", "status")); err != nil || r.Code != 0 {
		envLogf(env, "gh not authed, skipping ssh-key upload (run `gh ssh-key add` manually later)\n")
		return p
	}
	p.host = shortHostname()
	p.signPub = p.pub
	if def := filepath.Join(env.Home, ".ssh", "id_ed25519.pub"); fileExists(def) {
		p.signPub = def
	}
	if env.FreshKey { // B:424-438 ssh_force_upload: skip both "already set up" branches
		p.mode = ghUpload
	} else if ghSSHAuthenticates(ctx, env) {
		envLogf(env, "SSH already authenticates to GitHub, skipping key upload (already set up)\n")
		p.mode = ghRegistered
	} else if existing := ghLocalKeyOnGitHub(ctx, env); existing != "" {
		envLogf(env, "A local SSH key is already on GitHub (%s), skipping upload.\n", filepath.Base(existing))
		p.mode = ghRegistered
	} else {
		p.mode = ghUpload
	}
	return p
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}

// ghSSHAuthenticates ports B:549-557: GitHub's success banner comes with exit 1.
func ghSSHAuthenticates(ctx context.Context, env *Env) bool {
	r, err := env.Run.Run(ctx, dgCmd("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5",
		"-o", "StrictHostKeyChecking=accept-new", "-T", "git@github.com"))
	if err != nil {
		return false
	}
	return strings.Contains(r.Stdout+r.Stderr, "successfully authenticated")
}

var scopesRe = regexp.MustCompile(`Token scopes:.*`)

// ghHasScope ports B:504-507: first "Token scopes:" line of `gh auth status`.
func ghHasScope(ctx context.Context, env *Env, scope string) bool {
	r, err := env.Run.Run(ctx, dgCmd("gh", "auth", "status"))
	if err != nil {
		return false
	}
	line := scopesRe.FindString(r.Stdout + "\n" + r.Stderr)
	return strings.Contains(line, "'"+scope+"'")
}

func pubBlob(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	f := strings.Fields(string(b))
	if len(f) < 2 {
		return ""
	}
	return f[1]
}

// ghKeyRegistered ports key_on_gh_auth / key_on_gh_signing (B:522-529).
func ghKeyRegistered(ctx context.Context, env *Env, endpoint, pub string) bool {
	blob := pubBlob(pub)
	if blob == "" {
		return false
	}
	r, err := env.Run.Run(ctx, dgCmd("gh", "api", endpoint, "--jq", ".[].key"))
	return err == nil && strings.Contains(r.Stdout, blob)
}

// ghLocalKeyOnGitHub ports B:592-603.
func ghLocalKeyOnGitHub(ctx context.Context, env *Env) string {
	r, err := env.Run.Run(ctx, dgCmd("gh", "api", "user/keys", "--jq", ".[].key"))
	if err != nil || r.Code != 0 || strings.TrimSpace(r.Stdout) == "" {
		return ""
	}
	pubs, _ := filepath.Glob(filepath.Join(env.Home, ".ssh", "*.pub"))
	for _, pub := range pubs {
		if blob := pubBlob(pub); blob != "" && strings.Contains(r.Stdout, blob) {
			return pub
		}
	}
	return ""
}

type ghSigning int

const (
	sigDone    ghSigning = iota // nothing to do (no key, or already on GitHub)
	sigNoScope                  // gh lacks the signing scope: the user must upload by hand
	sigUpload                   // scope present, key missing on GitHub
)

// ghSigningState ports ensure_signing_key_on_github (B:571-586) without its side effects.
func ghSigningState(ctx context.Context, env *Env, p ghPlan) ghSigning {
	if !fileExists(p.signPub) {
		return sigDone
	}
	if !ghHasScope(ctx, env, scopeSigning) {
		return sigNoScope
	}
	if ghKeyRegistered(ctx, env, "user/ssh_signing_keys", p.signPub) {
		return sigDone
	}
	return sigUpload
}

// ghEnsureSigning uploads the signing key, or says how to when the scope is missing.
func ghEnsureSigning(ctx context.Context, env *Env, p ghPlan) {
	switch ghSigningState(ctx, env, p) {
	case sigNoScope:
		envLogf(env, "  ! commit-signing key not uploaded: gh lacks the %s scope.\n", scopeSigning)
		envLogf(env, "    Commits sign locally but show \"Unverified\" on GitHub until you run:\n")
		envLogf(env, "      gh auth refresh -h github.com -s %s\n", scopeSigning)
		envLogf(env, "      gh ssh-key add %s --title \"%s-sign\" --type signing\n", p.signPub, p.host)
	case sigUpload:
		ghAdd(ctx, env, p.signPub, p.host+"-sign", true)
	case sigDone:
		if fileExists(p.signPub) {
			envLogf(env, "  signing key already on GitHub (%s)\n", filepath.Base(p.signPub))
		}
	}
}

// ghAdd ports add_gh_ssh_key (B:533-544). A failure is logged, never returned.
func ghAdd(ctx context.Context, env *Env, pub, title string, signing bool) {
	args := []string{"ssh-key", "add", pub, "--title", title}
	if signing {
		args = append(args, "--type", "signing")
	}
	r, err := env.Run.Run(ctx, dgCmd("gh", args...))
	if err == nil && r.Code == 0 {
		envLogf(env, "  uploaded: %s\n", title)
		return
	}
	env.Warnings++
	envLogf(env, "  %s: upload failed\n", title)
	for _, line := range strings.Split(strings.TrimSpace(r.Stdout+r.Stderr), "\n") {
		if line != "" {
			envLogf(env, "    gh said: %s\n", line)
		}
	}
}

// ghUploadAll ports B:633-747.
func ghUploadAll(ctx context.Context, env *Env, p ghPlan) {
	if ghEnsureScopes(ctx, env) {
		ghUploadKeys(ctx, env, p)
	} else {
		ghManualUpload(env, p)
	}
	ghPostUpload(ctx, env, p)
}

// ghEnsureScopes refreshes missing token scopes. It reports false when the
// upload has to be done by hand.
func ghEnsureScopes(ctx context.Context, env *Env) bool {
	var missing []string
	for _, s := range []string{scopePublic, scopeSigning} {
		if !ghHasScope(ctx, env, s) {
			missing = append(missing, s)
		}
	}
	if len(missing) == 0 {
		return true
	}
	envLogf(env, "==> gh auth is missing scope(s): %s\n", strings.Join(missing, " "))
	if os.Getenv("GITHUB_TOKEN") != "" {
		envLogf(env, "    gh is authed via the GITHUB_TOKEN env var, scopes can't be refreshed on an env token, so falling back to manual upload.\n")
		return false
	}
	// Interactive: the device code and URL must reach the user.
	refresh := dgCmd("gh", "auth", "refresh", "-h", "github.com", "-s", strings.Join(missing, ","))
	refresh.Interactive = true
	if r, err := env.Run.Run(ctx, refresh); err != nil || r.Code != 0 {
		envLogf(env, "    gh auth refresh failed, falling back to manual upload.\n")
		return false
	}
	return true
}

func ghUploadKeys(ctx context.Context, env *Env, p ghPlan) {
	if ghKeyRegistered(ctx, env, "user/keys", p.pub) {
		envLogf(env, "  auth key already on GitHub: matching %s\n", p.host)
	} else {
		ghAdd(ctx, env, p.pub, p.host, false)
	}
	if ghKeyRegistered(ctx, env, "user/ssh_signing_keys", p.signPub) {
		envLogf(env, "  signing key already on GitHub: matching %s-sign\n", p.host)
	} else {
		ghAdd(ctx, env, p.signPub, p.host+"-sign", true)
	}
}

func ghManualUpload(env *Env, p ghPlan) {
	env.Warnings++
	envLogf(env, "==> manual SSH key upload:\n")
	envLogf(env, "    Visit https://github.com/settings/ssh/new (auth tab)\n")
	envLogf(env, "    and https://github.com/settings/ssh/signing/new (signing tab)\n")
	envLogf(env, "    Paste each block below, once as auth key, once as signing key:\n")
	envLogf(env, "  -- %s --\n", p.host)
	if b, err := os.ReadFile(p.pub); err == nil {
		envLogf(env, "%s\n", strings.TrimRight(string(b), "\n"))
	}
	envLogf(env, "    Then re-run if you want the gh git_protocol switch to ssh\n")
	envLogf(env, "    (or run it manually: gh config set git_protocol ssh --host github.com)\n")
}

// ghPostUpload flips the gh protocol, seeds ~/.ssh/config and moves an https origin to ssh.
func ghPostUpload(ctx context.Context, env *Env, p ghPlan) {
	if r, err := env.Run.Run(ctx, dgCmd("gh", "config", "set", "git_protocol", "ssh", "--host", "github.com")); err == nil && r.Code == 0 {
		envLogf(env, "gh: default protocol switched to ssh\n")
	}

	envLogf(env, "==> seeding github.com Host -> %s_ed25519 in ~/.ssh/config\n", p.sshHost)
	if err := ghSeedSSHConfig(env.Home, p.sshHost+"_ed25519"); err != nil {
		env.Warnings++
		envLogf(env, "could not seed ~/.ssh/config: %v\n", err)
	}

	r, err := env.Run.Run(ctx, dgCmd("git", "-C", env.RepoRoot, "remote", "get-url", "origin"))
	if err != nil {
		return
	}
	current := strings.TrimSpace(r.Stdout)
	if rest, ok := strings.CutPrefix(current, "https://github.com/"); ok {
		sshRemote := "git@github.com:" + strings.TrimSuffix(rest, ".git") + ".git"
		if res, err := env.Run.Run(ctx, dgCmd("git", "-C", env.RepoRoot, "remote", "set-url", "origin", sshRemote)); err == nil && res.Code == 0 {
			envLogf(env, "%s remote: HTTPS -> SSH (%s)\n", env.RepoRoot, sshRemote)
		}
	}
}

func ghSeedSSHConfig(home, hostkey string) error {
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, "config")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.WriteFile(path, existing, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	if strings.Contains(string(existing), hostkey) {
		return nil
	}
	block := "\nHost github.com\n    HostName github.com\n    User git\n    IdentityFile ~/.ssh/id_ed25519\n    IdentityFile ~/.ssh/" + hostkey + "\n"
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(block)
	return err
}
