package keys

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
)

// fakeKeyBox is a fakeBox that also rotates its own key.
type fakeKeyBox struct {
	fakeBox
	account    string
	cur        string // the key's public key on the box
	staged     string
	checkFail  int // CheckGitHub fails this many times first
	promoteErr error
	newKeys    int
	checks     []bool
}

func (b *fakeKeyBox) GitHubAccount() string { return b.account }
func (b *fakeKeyBox) CurrentPub(context.Context) (string, error) {
	return b.cur, nil
}
func (b *fakeKeyBox) NewKey(context.Context) (string, error) {
	b.newKeys++
	if b.staged == "" {
		b.staged = "ssh-ed25519 NEW" + b.name + " " + b.name
	}
	return b.staged, nil
}
func (b *fakeKeyBox) CheckGitHub(_ context.Context, rotating bool) error {
	b.checks = append(b.checks, rotating)
	if b.checkFail > 0 {
		b.checkFail--
		return errors.New("github refused " + b.name)
	}
	return nil
}
func (b *fakeKeyBox) PromoteKey(_ context.Context, newPub string) error {
	if b.promoteErr != nil {
		return b.promoteErr
	}
	b.cur, b.staged = newPub, ""
	return nil
}

const jqIDKeyTitle = `.[] | "\(.id) \(.key) \(.title)"`

func ghListing(f *fakeBW, endpoint string, lines ...string) {
	f.On("gh", "api", "--paginate", endpoint, "--jq", jqIDKeyTitle).Return(run.Result{Stdout: strings.Join(lines, "\n") + "\n"})
}

func ghCalls(f *fakeBW) []run.Cmd {
	var out []run.Cmd
	for _, c := range f.Calls() {
		if c.Name == "gh" {
			out = append(out, c)
		}
	}
	return out
}

func TestRotateBoxesSkipsFailingBoxThenResumes(t *testing.T) {
	o, f, _ := rotateHome(t)
	var log []string
	good := &fakeKeyBox{fakeBox: fakeBox{name: "good", scope: "work/acme", log: &log}, account: "acct", cur: "ssh-ed25519 OLDgood good"}
	bad := &fakeKeyBox{fakeBox: fakeBox{name: "bad", scope: "work/acme", log: &log}, cur: "ssh-ed25519 OLDbad bad", checkFail: 1}
	plain := &fakeBox{name: "plain", scope: "work/acme", log: &log}
	withInventory(t, good, bad, plain)

	f.On("gh", "auth", "token", "--hostname", "github.com", "--user", "acct").Return(run.Result{Stdout: "sekrit\n"})
	ghListing(f, "user/keys", "11 ssh-ed25519 OLDgood athome-good", "12 ssh-ed25519 OLDbad athome-bad", "13 ssh-ed25519 OTHER athome-x")
	ghListing(f, "user/ssh_signing_keys", "21 ssh-ed25519 OLDgood athome-good-sign", "22 ssh-ed25519 OLDbad athome-bad-sign")

	err := Rotate(context.Background(), o, Kinds{Boxes: true})
	var inc *IncompleteError
	if !errors.As(err, &inc) || !slices.Equal(inc.Skipped, []string{"bad"}) || !strings.Contains(err.Error(), "--resume") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(o.Out.(fmt.Stringer).String(), "plain") {
		t.Errorf("no note for the box without KeyBox: %s", o.Out)
	}
	if good.cur != "ssh-ed25519 NEWgood good" || bad.cur != "ssh-ed25519 OLDbad bad" {
		t.Errorf("cur: good %q bad %q", good.cur, bad.cur)
	}
	var deleted []string
	for _, c := range ghCalls(f) {
		if len(c.Args) > 2 && c.Args[1] == "-X" {
			deleted = append(deleted, c.Args[3])
		}
	}
	if !slices.Equal(deleted, []string{"user/keys/11", "user/ssh_signing_keys/21"}) {
		t.Errorf("deleted = %v", deleted)
	}
	st, _ := loadState(o.Home)
	if len(st.Jobs) != 1 || st.Jobs[0].Kind != kindBox || st.Jobs[0].Item != "bad" {
		t.Fatalf("state = %+v", st.Jobs)
	}
	if j := st.Jobs[0]; j.Targets["github"] != phaseAdded || j.Targets["box:bad"] != phaseSkipped || j.OldPub != "ssh-ed25519 OLDbad bad" || j.NewPub != "ssh-ed25519 NEWbad bad" || j.Promoted {
		t.Errorf("job = %+v", j)
	}

	if err := Resume(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if good.newKeys != 1 || bad.newKeys != 1 {
		t.Errorf("NewKey calls: good %d bad %d", good.newKeys, bad.newKeys)
	}
	if bad.cur != "ssh-ed25519 NEWbad bad" || !slices.Equal(bad.checks, []bool{true, true}) {
		t.Errorf("bad: cur %q checks %v", bad.cur, bad.checks)
	}
	deleted = nil
	for _, c := range ghCalls(f) {
		if len(c.Args) > 2 && c.Args[1] == "-X" {
			deleted = append(deleted, c.Args[3])
		}
	}
	if !slices.Equal(deleted, []string{"user/keys/11", "user/ssh_signing_keys/21", "user/keys/12", "user/ssh_signing_keys/22"}) {
		t.Errorf("deleted after resume = %v", deleted)
	}
	if st, _ := loadState(o.Home); len(st.Jobs) != 0 {
		t.Errorf("jobs left: %+v", st.Jobs)
	}
}

func TestRotateBoxesGitHubEnv(t *testing.T) {
	o, f, _ := rotateHome(t)
	var log []string
	acct := &fakeKeyBox{fakeBox: fakeBox{name: "acct", scope: "work/a", log: &log}, account: "acct", cur: "ssh-ed25519 OLD1 a"}
	none := &fakeKeyBox{fakeBox: fakeBox{name: "none", scope: "work/a", log: &log}, cur: "ssh-ed25519 OLD2 n"}
	withInventory(t, acct, none)
	f.On("gh", "auth", "token", "--hostname", "github.com", "--user", "acct").Return(run.Result{Stdout: "sekrit\n"})

	if err := Rotate(context.Background(), o, Kinds{Boxes: true}); err != nil {
		t.Fatal(err)
	}
	var adds, tokenAsks int
	for _, c := range ghCalls(f) {
		line := strings.Join(c.Args, " ")
		if strings.Contains(line, "sekrit") {
			t.Errorf("token in args: %s", line)
		}
		if slices.Equal(c.Args[:2], []string{"auth", "token"}) {
			tokenAsks++
			continue
		}
		hasEnv := slices.Contains(c.Env, "GH_TOKEN=sekrit")
		if strings.Contains(line, "athome-acct") {
			adds++
			if !hasEnv {
				t.Errorf("no GH_TOKEN on %s", line)
			}
		}
		if strings.Contains(line, "athome-none") && hasEnv {
			t.Errorf("GH_TOKEN on the accountless box: %s", line)
		}
	}
	if adds != 2 || tokenAsks != 1 {
		t.Errorf("adds %d, token asks %d", adds, tokenAsks)
	}
	if strings.Contains(o.Out.(fmt.Stringer).String(), "sekrit") {
		t.Error("token printed")
	}
}

func TestRotateBoxGoneFromInventoryIsDropped(t *testing.T) {
	o, _, _ := rotateHome(t)
	var log []string
	b := &fakeKeyBox{fakeBox: fakeBox{name: "b", scope: "work/a", log: &log}, cur: "ssh-ed25519 OLD b", checkFail: 1}
	withInventory(t, b)
	if err := Rotate(context.Background(), o, Kinds{Boxes: true}); err == nil {
		t.Fatal("want incomplete")
	}
	withInventory(t)
	if err := Resume(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(o.Out.(fmt.Stringer).String(), "no longer in the inventory") {
		t.Errorf("no note: %s", o.Out)
	}
	if st, _ := loadState(o.Home); len(st.Jobs) != 0 {
		t.Errorf("jobs left: %+v", st.Jobs)
	}
}

func deletes(f *fakeBW) []string {
	var out []string
	for _, c := range ghCalls(f) {
		if len(c.Args) > 2 && c.Args[1] == "-X" {
			out = append(out, c.Args[3])
		}
	}
	return out
}

func TestRotateBoxSkipsBoxClaimingLaptopKey(t *testing.T) {
	o, f, key := rotateHome(t)
	var log []string
	b := &fakeKeyBox{fakeBox: fakeBox{name: "evil", scope: "work/a", log: &log}, cur: strings.TrimSpace(readPub(t, key+".pub"))}
	withInventory(t, b)
	ghListing(f, "user/keys", "5 "+pubBlobLine(b.cur)+" athome-evil")
	ghListing(f, "user/ssh_signing_keys", "6 "+pubBlobLine(b.cur)+" athome-evil-sign")

	if err := Rotate(context.Background(), o, Kinds{Boxes: true}); err == nil {
		t.Fatal("want incomplete")
	}
	if d := deletes(f); len(d) != 0 {
		t.Errorf("deleted %v", d)
	}
	if b.newKeys != 0 {
		t.Errorf("NewKey called %d times", b.newKeys)
	}
}

func TestRotateBoxSkipsMalformedCurrentPub(t *testing.T) {
	for _, cur := range []string{"", "garbage", "ssh-rsa AAAA x", "ssh-ed25519 AAAA x\nssh-ed25519 BBBB y", "ssh-ed25519 A!B x"} {
		o, f, _ := rotateHome(t)
		var log []string
		b := &fakeKeyBox{fakeBox: fakeBox{name: "bad", scope: "work/a", log: &log}, cur: cur}
		withInventory(t, b)
		if err := Rotate(context.Background(), o, Kinds{Boxes: true}); err == nil {
			t.Errorf("%q: want incomplete", cur)
		}
		if len(ghCalls(f)) != 0 || b.newKeys != 0 {
			t.Errorf("%q: gh calls %v, new keys %d", cur, ghCalls(f), b.newKeys)
		}
	}
}

func TestRotateBoxDeletesOnlyOwnTitles(t *testing.T) {
	o, f, _ := rotateHome(t)
	var log []string
	b := &fakeKeyBox{fakeBox: fakeBox{name: "bx", scope: "work/a", log: &log}, cur: "ssh-ed25519 OLDbx bx"}
	withInventory(t, b)
	ghListing(f, "user/keys", "1 ssh-ed25519 OLDbx laptop", "2 ssh-ed25519 OLDbx athome-bx")
	ghListing(f, "user/ssh_signing_keys", "3 ssh-ed25519 OLDbx athome-bx", "4 ssh-ed25519 OLDbx athome-bx-sign")

	if err := Rotate(context.Background(), o, Kinds{Boxes: true}); err != nil {
		t.Fatal(err)
	}
	if d := deletes(f); !slices.Equal(d, []string{"user/keys/2", "user/ssh_signing_keys/4"}) {
		t.Errorf("deleted = %v", d)
	}
}

func pubBlobLine(pub string) string { return strings.Fields(pub)[0] + " " + pubBlob(pub) }

func TestRotateBoxRefusesANewKeyThatIsTheCurrentOne(t *testing.T) {
	o, f, _ := rotateHome(t)
	var log []string
	same := "ssh-ed25519 SAMEKEY same"
	b := &fakeKeyBox{fakeBox: fakeBox{name: "same", scope: "work/a", log: &log}, cur: same, staged: "ssh-ed25519 SAMEKEY other-comment"}
	withInventory(t, b)
	ghListing(f, "user/keys", "1 ssh-ed25519 SAMEKEY athome-same")
	ghListing(f, "user/ssh_signing_keys", "2 ssh-ed25519 SAMEKEY athome-same-sign")

	err := Rotate(context.Background(), o, Kinds{Boxes: true})
	var inc *IncompleteError
	if !errors.As(err, &inc) || !slices.Equal(inc.Skipped, []string{"same"}) {
		t.Fatalf("err = %v", err)
	}
	if d := deletes(f); len(d) != 0 {
		t.Errorf("deleted %v: the box's only registered key is gone", d)
	}
	if len(ghCalls(f)) != 0 || len(b.checks) != 0 {
		t.Errorf("gh calls %v, checks %v", ghCalls(f), b.checks)
	}
	if !strings.Contains(o.Out.(fmt.Stringer).String(), "is its current key") {
		t.Errorf("no reason: %s", o.Out)
	}
}

// alreadyRunner answers every gh ssh-key add with GitHub's "already in use".
type alreadyRunner struct{ run.Runner }

func (r alreadyRunner) Run(ctx context.Context, c run.Cmd) (run.Result, error) {
	if c.Name == "gh" && len(c.Args) > 1 && c.Args[0] == "ssh-key" && c.Args[1] == "add" {
		return run.Result{Code: 1, Stderr: "HTTP 422: key is already in use"}, nil
	}
	return r.Runner.Run(ctx, c)
}

// GitHub answers "already" for a key that is on a different account.
func TestRotateBoxFailsWhenTheKeyIsOnAnotherAccount(t *testing.T) {
	o, f, _ := rotateHome(t)
	o.Run = alreadyRunner{f}
	var log []string
	b := &fakeKeyBox{fakeBox: fakeBox{name: "bx", scope: "work/a", log: &log}, cur: "ssh-ed25519 OLDbx bx"}
	withInventory(t, b)
	f.On("gh", "api", "--paginate", "user/keys", "--jq", `.[] | "\(.id) \(.key)"`).Return(run.Result{Stdout: "1 ssh-ed25519 OLDbx\n"})
	f.On("gh", "api", "--paginate", "user/ssh_signing_keys", "--jq", `.[] | "\(.id) \(.key)"`).Return(run.Result{Stdout: "2 ssh-ed25519 OLDbx\n"})

	if err := Rotate(context.Background(), o, Kinds{Boxes: true}); err == nil || !strings.Contains(err.Error(), "may be on another account") {
		t.Fatalf("err = %v", err)
	}
	if len(b.checks) != 0 || len(deletes(f)) != 0 || b.cur != "ssh-ed25519 OLDbx bx" {
		t.Errorf("went on: checks %v deletes %v cur %q", b.checks, deletes(f), b.cur)
	}
}

func TestRotateBoxAlreadyOnTheAccountIsFine(t *testing.T) {
	o, f, _ := rotateHome(t)
	o.Run = alreadyRunner{f}
	var log []string
	b := &fakeKeyBox{fakeBox: fakeBox{name: "bx", scope: "work/a", log: &log}, cur: "ssh-ed25519 OLDbx bx"}
	withInventory(t, b)
	// the retry finds the new key on the account already
	f.On("gh", "api", "--paginate", "user/keys", "--jq", `.[] | "\(.id) \(.key)"`).Return(run.Result{Stdout: "1 ssh-ed25519 NEWbx\n"})
	f.On("gh", "api", "--paginate", "user/ssh_signing_keys", "--jq", `.[] | "\(.id) \(.key)"`).Return(run.Result{Stdout: "2 ssh-ed25519 NEWbx\n"})
	if err := Rotate(context.Background(), o, Kinds{Boxes: true}); err != nil {
		t.Fatal(err)
	}
	if b.cur != "ssh-ed25519 NEWbx bx" {
		t.Errorf("cur = %q", b.cur)
	}
}
