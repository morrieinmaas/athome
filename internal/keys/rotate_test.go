package keys

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/morrieinmaas/athome/internal/run"
)

const jqIDKey = `.[] | "\(.id) \(.key)"`

type fakeBox struct {
	name, scope string
	down        bool
	failOnce    bool
	reject      string // Authorize fails for a key whose line contains this
	authorized  []string
	log         *[]string
}

func (b *fakeBox) Name() string  { return b.name }
func (b *fakeBox) Scope() string { return b.scope }
func (b *fakeBox) note(s string) { *b.log = append(*b.log, b.name+" "+s) }
func (b *fakeBox) Authorize(_ context.Context, pub string) error {
	if b.reject != "" && strings.Contains(pub, b.reject) {
		return errors.New("rejected")
	}
	if b.failOnce {
		b.failOnce = false
		return errors.New("flaky")
	}
	if b.down {
		return errors.New("unreachable")
	}
	b.note("authorize")
	b.authorized = append(b.authorized, pub)
	return nil
}
func (b *fakeBox) Login(_ context.Context, key string) error {
	if b.down {
		return errors.New("unreachable")
	}
	b.note("login")
	return nil
}
func (b *fakeBox) Reset(_ context.Context, managed []string) error {
	if b.down {
		return errors.New("unreachable")
	}
	b.note("reset")
	b.authorized = slices.Clone(managed)
	return nil
}

func withInventory(t *testing.T, boxes ...Box) {
	t.Helper()
	old := Inventory
	Inventory = func(string) ([]Box, error) { return boxes, nil }
	t.Cleanup(func() { Inventory = old })
}

// rotateHome makes a temp home with a real laptop key and a fake runner that
// answers gh and ssh the way a working GitHub would.
func rotateHome(t *testing.T) (Options, *fakeBW, string) {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
	home := t.TempDir()
	ssh := filepath.Join(home, ".ssh")
	os.MkdirAll(ssh, 0o700)
	f := newFakeBW(t)
	key := filepath.Join(ssh, "h_ed25519")
	if _, err := keygen(context.Background(), f, key, "me@h"); err != nil {
		t.Fatal(err)
	}
	f.On("ssh", loginArgs(key+".rotate")...).Return(run.Result{Code: 1, Stderr: "Hi me! You've successfully authenticated, but GitHub does not provide shell access."})
	f.Have("chezmoi")
	o := Options{Home: home, Host: "h", Run: f, Out: &bytes.Buffer{}, Now: func() time.Time { return time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC) }}
	return o, f, key
}

func loginArgs(key string) []string {
	return []string{"-F", "/dev/null", "-i", key, "-o", "IdentitiesOnly=yes", "-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10", "-o", "StrictHostKeyChecking=accept-new", "-T", "git@github.com"}
}

func readPub(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

// cmdLines renders the recorded calls, keygen paths shortened.
func cmdLines(f *fakeBW, home string) []string {
	var out []string
	for _, c := range f.Calls() {
		out = append(out, strings.ReplaceAll(c.Name+" "+strings.Join(c.Args, " "), home, "~"))
	}
	return out
}

func indexOf(lines []string, prefix string) int {
	for i, l := range lines {
		if l == prefix || strings.HasPrefix(l, prefix+" ") {
			return i
		}
	}
	return -1
}

func TestRotateLaptopOrder(t *testing.T) {
	o, f, key := rotateHome(t)
	oldPub := readPub(t, key+".pub")
	f.On("gh", "api", "--paginate", "user/keys", "--jq", jqIDKey).Return(run.Result{Stdout: "7 " + oldPub + "\n8 ssh-ed25519 OTHER\n"})
	f.On("gh", "api", "--paginate", "user/ssh_signing_keys", "--jq", jqIDKey).Return(run.Result{Stdout: "9 " + oldPub + "\n"})
	withInventory(t)

	if err := Rotate(context.Background(), o, Kinds{Laptop: true}); err != nil {
		t.Fatal(err)
	}
	lines := cmdLines(f, o.Home)
	order := []string{
		"ssh-keygen -q -t ed25519 -N  -C me@h -f ~/.ssh/h_ed25519.rotate",
		"rbw add ssh/h_ed25519.rotate",
		"gh ssh-key add ~/.ssh/h_ed25519.rotate.pub --title h",
		"gh ssh-key add ~/.ssh/h_ed25519.rotate.pub --title h-sign --type signing",
		"ssh -F /dev/null -i ~/.ssh/h_ed25519.rotate",
		"chezmoi apply ~/.config/git/allowed_signers",
		"rbw add ssh/h_ed25519",
		"rbw remove ssh/h_ed25519.rotate",
		"gh api -X DELETE user/keys/7",
		"gh api -X DELETE user/ssh_signing_keys/9",
	}
	last := -1
	for _, want := range order {
		i := indexOf(lines, want)
		if i <= last {
			t.Fatalf("%q at %d, want after %d\n%s", want, i, last, strings.Join(lines, "\n"))
		}
		last = i
	}
	if indexOf(lines, "gh api -X DELETE user/keys/8") >= 0 {
		t.Error("deleted a key that is not the old one")
	}
	if got := readPub(t, key+".pub"); got == oldPub {
		t.Error("the laptop key was not replaced")
	}
	for _, p := range []string{".old", ".old.pub", ".rotate", ".rotate.pub"} {
		if _, err := os.Stat(key + p); err == nil {
			t.Errorf("%s left behind", p)
		}
	}
	st, _ := loadState(o.Home)
	if len(st.Jobs) != 0 || st.LastRotated != "2026-11-17T00:00:00Z" {
		t.Errorf("state = %+v", st)
	}
	items := f.get("ssh/h_ed25519")
	priv, _ := os.ReadFile(key)
	if len(items) != 1 || items[0] != SSHKeyPassword+"\n"+strings.TrimRight(string(priv), "\n")+"\n" {
		t.Errorf("stored item does not hold the new key")
	}
}

func TestRotateLoginFailsThenResume(t *testing.T) {
	o, f, key := rotateHome(t)
	before, _ := os.ReadFile(key)
	f.On("ssh", loginArgs(key+".rotate")...).Return(run.Result{Code: 255, Stderr: "Permission denied (publickey)."})
	withInventory(t)

	err := Rotate(context.Background(), o, Kinds{Laptop: true})
	if err == nil || !strings.Contains(err.Error(), "does not log in") {
		t.Fatalf("err = %v", err)
	}
	if indexOf(cmdLines(f, o.Home), "gh api -X DELETE") >= 0 {
		t.Error("deleted on GitHub before the login check passed")
	}
	if after, _ := os.ReadFile(key); !bytes.Equal(before, after) {
		t.Error("laptop key changed after a failed check")
	}
	st, _ := loadState(o.Home)
	if len(st.Jobs) != 1 || st.Jobs[0].Targets["github"] != phaseAdded {
		t.Fatalf("state = %+v", st.Jobs)
	}
	if err := Rotate(context.Background(), o, Kinds{Laptop: true}); err == nil || !strings.Contains(err.Error(), "a rotation is unfinished") {
		t.Errorf("second rotate: %v", err)
	}

	f.On("ssh", loginArgs(key+".rotate")...).Return(run.Result{Code: 1, Stderr: "successfully authenticated"})
	if err := Resume(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, l := range cmdLines(f, o.Home) {
		if strings.HasPrefix(l, "ssh-keygen -q -t") {
			n++
		}
	}
	if n != 2 { // one for the fixture, one for the rotation
		t.Errorf("ssh-keygen -t ran %d times, want 2", n)
	}
	if st, _ := loadState(o.Home); len(st.Jobs) != 0 {
		t.Errorf("job left after resume: %+v", st.Jobs)
	}
	if strings.Count(strings.Join(cmdLines(f, o.Home), "\n"), "gh ssh-key add") != 2 {
		t.Error("resume re-added the key on GitHub")
	}
}

func TestRotateGhAddFails(t *testing.T) {
	o, f, _ := rotateHome(t)
	f.On("gh", "ssh-key", "add", filepath.Join(o.Home, ".ssh", "h_ed25519.rotate.pub"), "--title", "h").Return(run.Result{Code: 1, Stderr: "HTTP 404"})
	withInventory(t)
	err := Rotate(context.Background(), o, Kinds{Laptop: true})
	if err == nil || !strings.Contains(err.Error(), "gh auth refresh -h github.com -s admin:public_key,admin:ssh_signing_key") {
		t.Fatalf("err = %v", err)
	}
	if indexOf(cmdLines(f, o.Home), "gh api -X DELETE") >= 0 {
		t.Error("deleted on GitHub")
	}
}

func TestRotateBoxesSkipAndResume(t *testing.T) {
	o, f, key := rotateHome(t)
	var log []string
	b1 := &fakeBox{name: "b1", scope: "work/acme", authorized: []string{"ssh-ed25519 HANDADDED x"}, log: &log}
	b2 := &fakeBox{name: "b2", scope: "work/acme", down: true, log: &log}
	withInventory(t, b1, b2)

	err := Rotate(context.Background(), o, Kinds{Laptop: true})
	var inc *IncompleteError
	if !errors.As(err, &inc) || !slices.Equal(inc.Skipped, []string{"b2"}) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "--resume") {
		t.Errorf("no resume hint: %v", err)
	}
	newPub := readPub(t, key+".pub")
	if !slices.Equal(b1.authorized, []string{newPub}) {
		t.Errorf("b1 authorized = %v, want only the managed set", b1.authorized)
	}
	if i, j := slices.Index(log, "b1 login"), slices.Index(log, "b1 reset"); i < 0 || j < i {
		t.Errorf("reset before login: %v", log)
	}
	if _, err := os.Stat(key + ".old"); err != nil {
		t.Error("old key removed while a box still needs it")
	}
	st, _ := loadState(o.Home)
	if st.Jobs[0].Targets["box:b2"] != phaseSkipped || st.Jobs[0].Targets["github"] != phaseRemoved {
		t.Errorf("targets = %v", st.Jobs[0].Targets)
	}

	b2.down = false
	if err := Resume(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(b2.authorized, []string{newPub}) {
		t.Errorf("b2 authorized = %v", b2.authorized)
	}
	if _, err := os.Stat(key + ".old"); err == nil {
		t.Error("old key kept after the last box")
	}
	if st, _ := loadState(o.Home); len(st.Jobs) != 0 {
		t.Errorf("job left: %+v", st.Jobs)
	}
	_ = f
}

func TestRotateTwiceOneItem(t *testing.T) {
	o, f, _ := rotateHome(t)
	withInventory(t)
	if err := Rotate(context.Background(), o, Kinds{Laptop: true}); err != nil {
		t.Fatal(err)
	}
	if err := Rotate(context.Background(), o, Kinds{Laptop: true}); err != nil {
		t.Fatal(err)
	}
	if n := len(f.get("ssh/h_ed25519")); n != 1 {
		t.Errorf("%d items, want 1", n)
	}
	calls := strings.Join(f.bwCalls, "|")
	if !strings.Contains(calls, "add ssh/h_ed25519") || !strings.Contains(calls, "edit ssh/h_ed25519") {
		t.Errorf("calls = %s", calls)
	}
}

func TestRotateEmptyInventoryNotes(t *testing.T) {
	o, _, _ := rotateHome(t)
	withInventory(t)
	if err := Rotate(context.Background(), o, Kinds{Laptop: true, Boxes: true, BreakGlass: true}); err != nil {
		t.Fatal(err)
	}
	out := o.Out.(*bytes.Buffer).String()
	for _, want := range []string{"no machines in the inventory", "no scopes in the inventory", "laptop key rotated"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestResumeNothing(t *testing.T) {
	o, _, _ := rotateHome(t)
	if err := Resume(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(o.Out.(*bytes.Buffer).String(), "nothing to resume") {
		t.Error("no note")
	}
}

func TestCorruptState(t *testing.T) {
	o, f, _ := rotateHome(t)
	os.MkdirAll(filepath.Dir(StatePath(o.Home)), 0o755)
	os.WriteFile(StatePath(o.Home), []byte("{"), 0o600)
	err := Rotate(context.Background(), o, Kinds{Laptop: true})
	if err == nil || !strings.Contains(err.Error(), "rotate.json") {
		t.Fatalf("err = %v", err)
	}
	if indexOf(cmdLines(f, o.Home), "gh") >= 0 {
		t.Error("ran gh with a corrupt state file")
	}
}

func TestResumeOldKeyAlreadyGone(t *testing.T) {
	o, f, _ := rotateHome(t)
	withInventory(t)
	// GitHub lists neither blob: nothing to delete, and that is fine.
	if err := Rotate(context.Background(), o, Kinds{Laptop: true}); err != nil {
		t.Fatal(err)
	}
	if indexOf(cmdLines(f, o.Home), "gh api -X DELETE") >= 0 {
		t.Error("deleted a key GitHub did not list")
	}
}

func TestRotateRemovalListFails(t *testing.T) {
	o, f, _ := rotateHome(t)
	f.On("gh", "api", "--paginate", "user/keys", "--jq", jqIDKey).Return(run.Result{Code: 1, Stderr: "HTTP 500"})
	withInventory(t)
	if err := Rotate(context.Background(), o, Kinds{Laptop: true}); err == nil {
		t.Fatal("want an error")
	}
	st, _ := loadState(o.Home)
	if len(st.Jobs) != 1 || st.Jobs[0].Targets["github"] != phaseChecked || !st.Jobs[0].Installed {
		t.Errorf("state = %+v", st.Jobs)
	}
}

func TestRotatePrefersIDKey(t *testing.T) {
	o, f, _ := rotateHome(t)
	id := filepath.Join(o.Home, ".ssh", "id_ed25519")
	if _, err := keygen(context.Background(), f, id, "me@h"); err != nil {
		t.Fatal(err)
	}
	f.On("ssh", loginArgs(id+".rotate")...).Return(run.Result{Code: 1, Stderr: "successfully authenticated"})
	withInventory(t)
	if err := Rotate(context.Background(), o, Kinds{Laptop: true}); err != nil {
		t.Fatal(err)
	}
	if len(f.get("ssh/id_ed25519")) != 1 || len(f.get("ssh/h_ed25519")) != 0 {
		t.Error("rotated the wrong key")
	}
}

func TestBreakGlass(t *testing.T) {
	o, f, _ := rotateHome(t)
	pub, err := NewBreakGlass(context.Background(), f, "work/acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.get("athome/work/acme/break-glass")) != 1 {
		t.Fatal("not stored")
	}
	got, err := BreakGlassPub(context.Background(), f, "work/acme")
	if err != nil || pubBlob(got) != pubBlob(pub) {
		t.Errorf("BreakGlassPub = %q, %v; want blob of %q", got, err, pub)
	}
	if m, _ := filepath.Glob(filepath.Join(os.TempDir(), "athome-*")); len(m) > 0 {
		t.Errorf("temp left: %v", m)
	}
	if _, err := BreakGlassPub(context.Background(), f, "work/none"); !errors.Is(err, ErrNoItem) {
		t.Errorf("missing item: %v", err)
	}
	_ = o
}

func TestRotateBreakGlassPerScope(t *testing.T) {
	o, f, key := rotateHome(t)
	var log []string
	a := &fakeBox{name: "a", scope: "work/acme", log: &log}
	b := &fakeBox{name: "b", scope: "sidebiz/shop", log: &log}
	withInventory(t, a, b)
	if err := Rotate(context.Background(), o, Kinds{BreakGlass: true}); err != nil {
		t.Fatal(err)
	}
	laptop := readPub(t, key+".pub")
	for _, bx := range []*fakeBox{a, b} {
		bg, err := BreakGlassPub(context.Background(), f, bx.scope)
		if err != nil {
			t.Fatal(err)
		}
		if len(bx.authorized) != 2 || bx.authorized[0] != laptop || pubBlob(bx.authorized[1]) != pubBlob(bg) {
			t.Errorf("%s authorized = %v", bx.name, bx.authorized)
		}
	}
}

func TestRotateLockedRbwStopsBeforeAnything(t *testing.T) {
	o, f, _ := rotateHome(t)
	f.locked = true
	withInventory(t)
	if err := Rotate(context.Background(), o, Kinds{Laptop: true}); !errors.Is(err, ErrLocked) {
		t.Fatalf("err = %v", err)
	}
	if indexOf(cmdLines(f, o.Home), "gh") >= 0 {
		t.Error("ran gh with rbw locked")
	}
	if st, _ := loadState(o.Home); len(st.Jobs) != 0 {
		t.Errorf("a locked run left a job: %+v", st.Jobs)
	}
}

func TestBreakGlassPubLockedIsNotMissing(t *testing.T) {
	_, f, _ := rotateHome(t)
	if _, err := NewBreakGlass(context.Background(), f, "work/acme"); err != nil {
		t.Fatal(err)
	}
	f.locked = true
	if _, err := BreakGlassPub(context.Background(), f, "work/acme"); errors.Is(err, ErrNoItem) || err == nil {
		t.Errorf("locked rbw read as a missing item: %v", err)
	}
}

func TestRotateLoginFailsKeepsRealItem(t *testing.T) {
	o, f, key := rotateHome(t)
	f.put("ssh/h_ed25519", SSHKeyPassword+"\nold key\n")
	f.On("ssh", loginArgs(key+".rotate")...).Return(run.Result{Code: 255})
	withInventory(t)
	if err := Rotate(context.Background(), o, Kinds{Laptop: true}); err == nil {
		t.Fatal("want an error")
	}
	if got := f.get("ssh/h_ed25519"); len(got) != 1 || got[0] != SSHKeyPassword+"\nold key\n" {
		t.Errorf("real item changed before GitHub accepted the new key: %q", got)
	}
	if len(f.get("ssh/h_ed25519.rotate")) != 1 {
		t.Error("new key not in the staging item")
	}
}

func TestBreakGlassSkippedBoxKeepsOldItem(t *testing.T) {
	o, f, _ := rotateHome(t)
	oldPub, err := NewBreakGlass(context.Background(), f, "work/acme")
	if err != nil {
		t.Fatal(err)
	}
	var log []string
	a := &fakeBox{name: "a", scope: "work/acme", log: &log}
	b := &fakeBox{name: "b", scope: "work/acme", down: true, log: &log}
	withInventory(t, a, b)
	if err := Rotate(context.Background(), o, Kinds{BreakGlass: true}); err == nil {
		t.Fatal("want skipped")
	}
	if pub, _ := BreakGlassPub(context.Background(), f, "work/acme"); pubBlob(pub) != pubBlob(oldPub) {
		t.Error("real item replaced while box b still has only the old key")
	}
	b.down = false
	if err := Resume(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	pub, _ := BreakGlassPub(context.Background(), f, "work/acme")
	if pubBlob(pub) == pubBlob(oldPub) || len(f.get("athome/work/acme/break-glass.rotate")) != 0 {
		t.Error("new key not promoted after the last box")
	}
}

func TestResumeAfterCrashBetweenSwapAndSave(t *testing.T) {
	o, f, _ := rotateHome(t)
	f.On("gh", "api", "--paginate", "user/keys", "--jq", jqIDKey).Return(run.Result{Code: 1})
	withInventory(t)
	Rotate(context.Background(), o, Kinds{Laptop: true}) // stops at removal, swap done
	st, _ := loadState(o.Home)
	st.Jobs[0].Installed = false
	st.save(o.Home)
	f.On("gh", "api", "--paginate", "user/keys", "--jq", jqIDKey).Return(run.Result{})
	if err := Resume(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if st, _ := loadState(o.Home); len(st.Jobs) != 0 {
		t.Errorf("job left: %+v", st.Jobs)
	}
}

func TestResumeNullTargetsAndGoneBox(t *testing.T) {
	o, f, _ := rotateHome(t)
	withInventory(t)
	os.MkdirAll(filepath.Dir(StatePath(o.Home)), 0o755)
	os.WriteFile(StatePath(o.Home), []byte(`{"jobs":[{"kind":"break-glass","scope":"work/x","item":"athome/work/x/break-glass","new_pub":"ssh-ed25519 AAAA","stored":true,"targets":null},
	{"kind":"break-glass","scope":"work/y","item":"athome/work/y/break-glass","new_pub":"ssh-ed25519 BBBB","stored":true,"targets":{"box:gone":"skipped"}}]}`), 0o600)
	f.put("athome/work/x/break-glass.rotate", SSHKeyPassword+"\nk\n")
	f.put("athome/work/y/break-glass.rotate", SSHKeyPassword+"\nk\n")
	if err := Resume(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if st, _ := loadState(o.Home); len(st.Jobs) != 0 {
		t.Errorf("jobs left: %+v", st.Jobs)
	}
}

func TestRotateLocksOut(t *testing.T) {
	o, _, _ := rotateHome(t)
	withInventory(t)
	unlock, err := lockState(o.Home)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := Rotate(context.Background(), o, Kinds{Laptop: true}); err == nil || !strings.Contains(err.Error(), "another athome keys rotate") {
		t.Errorf("err = %v", err)
	}
}

func writeState(t *testing.T, home, body string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(StatePath(home)), 0o755)
	if err := os.WriteFile(StatePath(home), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestResumeSkipsFinishedJob(t *testing.T) {
	o, f, _ := rotateHome(t)
	withInventory(t)
	// Finished and promoted; its staging item is gone, so redoing it would fail.
	writeState(t, o.Home, `{"jobs":[{"kind":"break-glass","scope":"work/x","item":"athome/work/x/break-glass","new_pub":"ssh-ed25519 AAAA","stored":true,"promoted":true,"targets":{"box:a":"removed"}}]}`)
	if err := Resume(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if st, _ := loadState(o.Home); len(st.Jobs) != 0 {
		t.Errorf("jobs left: %+v", st.Jobs)
	}
	if indexOf(cmdLines(f, o.Home), "rbw get") >= 0 {
		t.Error("redid a finished job")
	}
}

func TestResumeBreakGlassCrashAfterStagingRemoved(t *testing.T) {
	o, f, _ := rotateHome(t)
	withInventory(t)
	pub, err := NewBreakGlass(context.Background(), f, "work/x")
	if err != nil {
		t.Fatal(err)
	}
	writeState(t, o.Home, `{"jobs":[{"kind":"break-glass","scope":"work/x","item":"athome/work/x/break-glass","new_pub":"`+pub+`","stored":true,"targets":{}}]}`)
	// The staging item is already gone and the real item holds the new key.
	if err := Resume(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if st, _ := loadState(o.Home); len(st.Jobs) != 0 {
		t.Errorf("job left: %+v", st.Jobs)
	}
	if got, _ := BreakGlassPub(context.Background(), f, "work/x"); pubBlob(got) != pubBlob(pub) {
		t.Error("real item changed")
	}
}

func TestResumeAfterCrashInsideSwap(t *testing.T) {
	o, f, key := rotateHome(t)
	f.On("gh", "api", "--paginate", "user/keys", "--jq", jqIDKey).Return(run.Result{Code: 1})
	withInventory(t)
	Rotate(context.Background(), o, Kinds{Laptop: true}) // swap done, stops at removal
	// Rewind to "new private key in place, its .pub not yet moved".
	os.Rename(key+".pub", key+".rotate.pub")
	st, _ := loadState(o.Home)
	st.Jobs[0].Installed = false
	st.save(o.Home)
	f.On("gh", "api", "--paginate", "user/keys", "--jq", jqIDKey).Return(run.Result{})
	if err := Resume(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if !exists(key+".pub") || exists(key+".rotate.pub") {
		t.Error("swap not finished")
	}
	if n := strings.Count(strings.Join(cmdLines(f, o.Home), "\n"), "chezmoi apply"); n != 2 {
		t.Errorf("allowed_signers re-rendered %d times, want 2 (the run and the finished swap)", n)
	}
	if st, _ := loadState(o.Home); len(st.Jobs) != 0 {
		t.Errorf("job left: %+v", st.Jobs)
	}
}

func TestRotateReplacesLeftoverStagedKey(t *testing.T) {
	o, _, key := rotateHome(t)
	withInventory(t)
	os.WriteFile(key+".rotate", []byte("stale"), 0o600)
	os.WriteFile(key+".rotate.pub", []byte("ssh-ed25519 STALE x"), 0o644)
	if err := Rotate(context.Background(), o, Kinds{Laptop: true}); err != nil {
		t.Fatal(err)
	}
	if pubBlob(readPub(t, key+".pub")) == "STALE" {
		t.Error("installed the stale leftover")
	}
}

func TestRotateWithoutChezmoiIsANote(t *testing.T) {
	o, f, _ := rotateHome(t)
	withInventory(t)
	o.Run = &noChezmoi{fakeBW: f}
	if err := Rotate(context.Background(), o, Kinds{Laptop: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(o.Out.(*bytes.Buffer).String(), "chezmoi not found") {
		t.Error("no note about chezmoi")
	}
}

type noChezmoi struct{ *fakeBW }

func (n *noChezmoi) LookPath(name string) (string, error) {
	if name == "chezmoi" {
		return "", errors.New("not found")
	}
	return n.fakeBW.LookPath(name)
}

func TestResumeLaptopResetKeepsNewBreakGlass(t *testing.T) {
	o, f, _ := rotateHome(t)
	if _, err := NewBreakGlass(context.Background(), f, "work/acme"); err != nil {
		t.Fatal(err)
	}
	var log []string
	b1 := &fakeBox{name: "b1", scope: "work/acme", failOnce: true, log: &log}
	b2 := &fakeBox{name: "b2", scope: "work/acme", down: true, log: &log}
	withInventory(t, b1, b2)
	Rotate(context.Background(), o, Kinds{Laptop: true, BreakGlass: true})
	st, _ := loadState(o.Home)
	var bgNew string
	for _, j := range st.Jobs {
		if j.Kind == kindBreakGlass {
			bgNew = j.NewPub
		}
	}
	if bgNew == "" || st.LastRotated != "" {
		t.Fatalf("state = %+v", st)
	}
	if err := Resume(context.Background(), o); err == nil {
		t.Fatal("b2 is still down")
	}
	found := false
	for _, k := range b1.authorized {
		found = found || pubBlob(k) == pubBlob(bgNew)
	}
	if !found {
		t.Errorf("laptop reset dropped the new break-glass key from b1: %v", b1.authorized)
	}
}

func TestLastRotatedSetWhenLaptopDoneDespiteSkippedBreakGlass(t *testing.T) {
	o, _, _ := rotateHome(t)
	var log []string
	// The box takes the laptop key but refuses the break-glass key, so only the laptop job finishes.
	withInventory(t, &fakeBox{name: "b", scope: "work/acme", reject: "break-glass", log: &log})
	if err := Rotate(context.Background(), o, Kinds{Laptop: true, BreakGlass: true}); err == nil {
		t.Fatal("want the break-glass job incomplete")
	}
	if st, _ := loadState(o.Home); st.LastRotated == "" {
		t.Error("last_rotated not set after the laptop rotation")
	}
}
