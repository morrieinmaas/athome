package steps_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
)

func chezmoiConfig(fx *fixture) string { return fx.Path(".config", "chezmoi", "chezmoi.toml") }

func TestSourceDirPin(t *testing.T) {
	seed := "[data]\n    machineType = \"personal\"\n"
	t.Run("prepended when absent", func(t *testing.T) {
		fx := newFixture(t)
		writeFile(t, chezmoiConfig(fx), seed, 0o600)
		fx.MustExec(steps.Finish())
		want := "sourceDir = \"" + filepath.Join(fx.Repo, "home") + "\"\n" + seed
		if got := readFile(t, chezmoiConfig(fx)); got != want {
			t.Errorf("chezmoi.toml =\n%s\nwant\n%s", got, want)
		}
	})
	t.Run("kept when already present", func(t *testing.T) {
		fx := newFixture(t)
		pinned := "sourceDir = \"/somewhere/else\"\n" + seed
		writeFile(t, chezmoiConfig(fx), pinned, 0o600)
		fx.MustExec(steps.Finish())
		if got := readFile(t, chezmoiConfig(fx)); got != pinned {
			t.Errorf("an existing sourceDir must not change:\n%s", got)
		}
	})
	t.Run("no config file, none created", func(t *testing.T) {
		fx := newFixture(t)
		fx.MustExec(steps.Finish())
		if exists(chezmoiConfig(fx)) {
			t.Errorf("Finish must not create chezmoi.toml")
		}
	})
	t.Run("second run is a no-op", func(t *testing.T) {
		fx := newFixture(t)
		writeFile(t, chezmoiConfig(fx), seed, 0o600)
		fx.MustExec(steps.Finish())
		first := readFile(t, chezmoiConfig(fx))
		fx.MustExec(steps.Finish())
		if got := readFile(t, chezmoiConfig(fx)); got != first || strings.Count(got, "sourceDir") != 1 {
			t.Errorf("the pin must appear exactly once:\n%s", got)
		}
	})
}

// F16: the pinned path is a TOML string, not a Go-quoted one.
func TestSourceDirPinEscapesTOML(t *testing.T) {
	fx := newFixture(t)
	fx.Env.RepoRoot = fx.Path("odd \\ \"q\" \a dir") // backslash, quotes and a BEL
	writeFile(t, chezmoiConfig(fx), "[data]\n    machineType = \"personal\"\n", 0o600)
	fx.MustExec([]steps.Step{steps.Finish()[0]})
	var doc struct {
		SourceDir string `toml:"sourceDir"`
	}
	if _, err := toml.DecodeFile(chezmoiConfig(fx), &doc); err != nil {
		t.Fatalf("chezmoi.toml is no longer valid TOML: %v\n%s", err, readFile(t, chezmoiConfig(fx)))
	}
	if want := filepath.Join(fx.Env.RepoRoot, "home"); doc.SourceDir != want {
		t.Errorf("sourceDir = %q, want %q", doc.SourceDir, want)
	}
}

func TestSourceDirPinRejectsNonUTF8(t *testing.T) {
	fx := newFixture(t)
	fx.Env.RepoRoot = fx.Path("bad\xffdir")
	seed := "[data]\n    machineType = \"personal\"\n"
	writeFile(t, chezmoiConfig(fx), seed, 0o600)
	_, err := fx.Exec([]steps.Step{steps.Finish()[0]})
	if err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("want an error naming UTF-8, got %v", err)
	}
	if got := readFile(t, chezmoiConfig(fx)); got != seed {
		t.Errorf("chezmoi.toml must stay untouched: %q", got)
	}
}

func hooksDir(fx *fixture) string { return filepath.Join(fx.Repo, ".git", "hooks") }

func TestHooksInstalled(t *testing.T) {
	fx := newFixture(t)
	fx.MustExec(steps.Finish())
	for name, marker := range map[string]string{"pre-commit": "# pre-commit", "commit-msg": "# commit-msg"} {
		p := filepath.Join(hooksDir(fx), name)
		if got := readFile(t, p); !strings.Contains(got, marker) {
			t.Errorf("%s = %q, want a copy of executable_%s", name, got, name)
		}
		if fi, err := os.Stat(p); err != nil || fi.Mode().Perm()&0o111 == 0 {
			t.Errorf("%s must be executable: %v", name, err)
		}
	}
}

func TestHooksSkipExisting(t *testing.T) {
	fx := newFixture(t)
	writeFile(t, filepath.Join(hooksDir(fx), "pre-commit"), "# mine\n", 0o755)
	fx.MustExec(steps.Finish())
	if got := readFile(t, filepath.Join(hooksDir(fx), "pre-commit")); got != "# mine\n" {
		t.Errorf("existing pre-commit overwritten: %q", got)
	}
	if exists(filepath.Join(hooksDir(fx), "commit-msg")) {
		t.Errorf("bash skips the whole hook install when pre-commit exists")
	}
}

func TestHooksNoGitDir(t *testing.T) {
	fx := newFixture(t)
	if err := os.RemoveAll(filepath.Join(fx.Repo, ".git")); err != nil {
		t.Fatal(err)
	}
	fx.MustExec(steps.Finish())
	if exists(filepath.Join(fx.Repo, ".git")) {
		t.Errorf("no .git directory, nothing to create")
	}
}

func bwSetupFixture(t *testing.T) (*fixture, string) {
	t.Helper()
	fx := newFixture(t)
	bw := fx.Path(".local", "bin", "bw-setup")
	writeFile(t, bw, "#!/bin/sh\nexit 0\n", 0o755)
	return fx, bw
}

func TestBitwardenNonInteractive(t *testing.T) {
	t.Run("rbw not set up: setup script not run", func(t *testing.T) {
		fx, bw := bwSetupFixture(t)
		fx.Fake.Have("rbw")
		fx.Fake.On("rbw", "unlocked").Return(run.Result{Code: 1})
		fx.MustExec(steps.Finish())
		if fx.mentions(bw) || fx.mentions("bw-setup") {
			t.Errorf("without a terminal bw-setup must never run:\n%s", fx.dump())
		}
		fi, err := os.Stat(fx.Path(".secrets"))
		if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
			t.Errorf("~/.secrets must exist with mode 0700: %v %v", fi, err)
		}
	})
	t.Run("useBitwarden=false still creates ~/.secrets", func(t *testing.T) {
		fx, _ := bwSetupFixture(t)
		fx.Env.Cfg.Data["useBitwarden"] = "false"
		fx.MustExec(steps.Finish())
		if fi, err := os.Stat(fx.Path(".secrets")); err != nil || fi.Mode().Perm() != 0o700 {
			t.Errorf("~/.secrets must exist with mode 0700: %v", err)
		}
		if fx.mentions("bw-setup") {
			t.Errorf("bw-setup must not run:\n%s", fx.dump())
		}
	})
	t.Run("rbw missing entirely", func(t *testing.T) {
		fx, _ := bwSetupFixture(t)
		fx.MustExec(steps.Finish())
		if !exists(fx.Path(".secrets")) {
			t.Errorf("~/.secrets must exist")
		}
	})
}

func TestSecretsRestoreSkip(t *testing.T) {
	restore := func(fx *fixture) string {
		p := fx.Path(".local", "bin", "secrets-restore")
		writeFile(t, p, "#!/bin/sh\nexit 0\n", 0o755)
		return p
	}
	t.Run("an existing ~/.secrets/*/.env skips", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.Have("rbw")
		p := restore(fx)
		writeFile(t, fx.Path(".secrets", "proj", ".env"), "A=1\n", 0o600)
		fx.MustExec(steps.Finish())
		if fx.mentions(p) || fx.mentions("secrets-restore") {
			t.Errorf("secrets already present, restore must not run:\n%s", fx.dump())
		}
	})
	t.Run("unlocked and empty: restore runs", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.Have("rbw")
		p := restore(fx)
		fx.MustExec(steps.Finish())
		if !fx.mentions(p) {
			t.Errorf("rbw unlocked and no .env: want a call to %s:\n%s", p, fx.dump())
		}
	})
	t.Run("rbw locked: restore never runs", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.Have("rbw")
		fx.Fake.On("rbw", "unlocked").Return(run.Result{Code: 1})
		p := restore(fx)
		fx.MustExec(steps.Finish())
		if fx.mentions(p) {
			t.Errorf("rbw locked, restore can never work:\n%s", fx.dump())
		}
	})
	t.Run("restore is interactive", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.Have("rbw")
		p := restore(fx)
		fx.MustExec(steps.Finish())
		if c := fx.callsTo(p); len(c) != 1 || !c[0].Interactive {
			t.Errorf("secrets-restore prompts, it must be interactive: %+v", c)
		}
	})
	t.Run("a failing restore is not an error", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.Have("rbw")
		p := restore(fx)
		fx.Fake.On(p).Return(run.Result{Code: 1})
		if _, err := fx.Exec(steps.Finish()); err != nil {
			t.Fatalf("secrets-restore trouble is a message, not an error: %v", err)
		}
		if fx.Env.Warnings != 0 {
			t.Errorf("Warnings = %d, a failing secrets-restore only logs (B:1026)", fx.Env.Warnings)
		}
	})
	t.Run("unlocked, empty, no script: a follow-up line", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.Have("rbw")
		fx.MustExec(steps.Finish())
		got, _ := os.ReadFile(steps.FollowupLogPath())
		if !strings.Contains(string(got), "Secrets: run `secrets-restore`") {
			t.Errorf("want the secrets-restore follow-up line (B:1028), log: %q", got)
		}
	})
	t.Run("unlocked, empty, script not executable: a follow-up line", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.Have("rbw")
		writeFile(t, fx.Path(".local", "bin", "secrets-restore"), "#!/bin/sh\n", 0o644)
		fx.MustExec(steps.Finish())
		got, _ := os.ReadFile(steps.FollowupLogPath())
		if !strings.Contains(string(got), "Secrets: run `secrets-restore`") {
			t.Errorf("want the secrets-restore follow-up line (B:1028), log: %q", got)
		}
	})
	t.Run("rbw locked: no follow-up line", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.Have("rbw")
		fx.Fake.On("rbw", "unlocked").Return(run.Result{Code: 1})
		fx.MustExec(steps.Finish())
		if got, _ := os.ReadFile(steps.FollowupLogPath()); len(got) != 0 {
			t.Errorf("a locked rbw says nothing here (B:1017), log: %q", got)
		}
	})
}

func headStub(fx *fixture, branch string) {
	fx.Fake.On("git", "-C", fx.Repo, "rev-parse", "--abbrev-ref", "HEAD").Return(run.Result{Stdout: branch + "\n"})
}

func TestDetachedHead(t *testing.T) {
	t.Run("detached: checkout main", func(t *testing.T) {
		fx := newFixture(t)
		headStub(fx, "HEAD")
		fx.MustExec(steps.Finish())
		if !fx.called("git", "checkout", "main") {
			t.Errorf("want `git checkout main`:\n%s", fx.dump())
		}
		if fx.called("git", "checkout", "-B", "main", "origin/main") {
			t.Errorf("the fallback must only run when the first checkout fails:\n%s", fx.dump())
		}
	})
	t.Run("checkout main fails: create it from origin/main", func(t *testing.T) {
		fx := newFixture(t)
		headStub(fx, "HEAD")
		fx.Fake.On("git", "-C", fx.Repo, "checkout", "main").Return(run.Result{Code: 1})
		fx.MustExec(steps.Finish())
		if !fx.called("git", "checkout", "-B", "main", "origin/main") {
			t.Errorf("want `git checkout -B main origin/main`:\n%s", fx.dump())
		}
	})
	t.Run("both fail: message, no error", func(t *testing.T) {
		fx := newFixture(t)
		headStub(fx, "HEAD")
		fx.Fake.On("git", "-C", fx.Repo, "checkout", "main").Return(run.Result{Code: 1})
		fx.Fake.On("git", "-C", fx.Repo, "checkout", "-B", "main", "origin/main").Return(run.Result{Code: 1})
		if _, err := fx.Exec(steps.Finish()); err != nil {
			t.Fatalf("bash only prints a red line here: %v", err)
		}
		if fx.Log.Len() == 0 {
			t.Errorf("want a log line telling the user to check git status")
		}
	})
	// Spec Build 1 criterion 7 excludes this: HEAD stays detached, so a rerun retries.
	t.Run("both fail: a rerun retries the checkout", func(t *testing.T) {
		fx := newFixture(t)
		headStub(fx, "HEAD")
		fx.Fake.On("git", "-C", fx.Repo, "checkout", "main").Return(run.Result{Code: 1})
		fx.Fake.On("git", "-C", fx.Repo, "checkout", "-B", "main", "origin/main").Return(run.Result{Code: 1})
		fx.MustExec(steps.Finish())
		rep := fx.MustExec(steps.Finish())
		if !slices.Contains(rep.Applied, "checkout-main") {
			t.Errorf("a still-detached HEAD must retry checkout-main, applied=%v", rep.Applied)
		}
	})
	t.Run("on a branch: no checkout", func(t *testing.T) {
		fx := newFixture(t)
		headStub(fx, "feature/x")
		fx.MustExec(steps.Finish())
		if fx.called("git", "checkout") {
			t.Errorf("not detached, nothing to restore:\n%s", fx.dump())
		}
	})
}

func TestCheckoutMainIsLast(t *testing.T) {
	list := steps.Finish()
	if len(list) == 0 || list[len(list)-1].Name != "checkout-main" {
		t.Fatalf("last Finish step must be checkout-main, got %v", names(list))
	}
}
