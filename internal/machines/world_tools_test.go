package machines

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/morrieinmaas/athome/internal/run"
)

func (w *world) gh(c run.Cmd) run.Result {
	for _, e := range c.Env {
		if v, ok := strings.CutPrefix(e, "GH_TOKEN="); ok {
			w.ghEnv = append(w.ghEnv, v)
		}
	}
	a := c.Args
	switch {
	case len(a) == 4 && a[0] == "api" && a[1] == "user" && a[2] == "--jq" && a[3] == ".login":
		w.add("gh-user")
		if w.ghLogin == "" {
			return run.Result{Code: 1, Stderr: "gh: not logged in"}
		}
		return run.Result{Stdout: w.ghLogin + "\n"}
	case len(a) > 1 && a[0] == "auth" && a[1] == "token":
		w.add("gh-token " + a[len(a)-1])
		return run.Result{Stdout: "tok-" + a[len(a)-1] + "\n"}
	case len(a) > 3 && a[0] == "api" && a[1] == "--paginate":
		var b strings.Builder
		for _, k := range w.ghKeys[a[2]] {
			fmt.Fprintf(&b, "%s %s %s\n", k.id, k.key, k.title)
		}
		return run.Result{Stdout: b.String()}
	case len(a) > 3 && a[0] == "ssh-key" && a[1] == "add":
		endpoint, op := "user/keys", "gh-add-auth"
		if slices.Contains(a, "signing") {
			endpoint, op = "user/ssh_signing_keys", "gh-add-sign"
		}
		if w.fail[op] {
			return run.Result{Code: 1, Stderr: "HTTP 404: Not Found (admin:public_key)"}
		}
		pub, _ := os.ReadFile(a[2])
		if w.ghForeign[strings.Fields(string(pub))[1]] {
			return run.Result{Code: 1, Stderr: "HTTP 422: key is already in use"}
		}
		w.ghNext++
		title := a[slices.Index(a, "--title")+1]
		w.ghKeys[endpoint] = append(w.ghKeys[endpoint], fakeGHKey{strconv.Itoa(w.ghNext), strings.Join(strings.Fields(string(pub))[:2], " "), title})
		w.ghOwner[strings.Fields(string(pub))[1]] = w.ghAccount(c)
		w.add(op + " " + title)
		return run.Result{}
	case len(a) == 4 && a[0] == "api" && a[1] == "-X" && a[2] == "DELETE":
		ep, id, _ := strings.Cut(strings.TrimPrefix(a[3], "user/"), "/")
		ep = "user/" + ep
		var keep []fakeGHKey
		for _, k := range w.ghKeys[ep] {
			if k.id == id {
				w.add("gh-delete " + ep + " " + k.title)
				continue
			}
			keep = append(keep, k)
		}
		w.ghKeys[ep] = keep
		return run.Result{}
	}
	return run.Result{Code: 2, Stderr: "fake gh: unexpected " + strings.Join(a, " ")}
}

// keyOwner is the account whose key this is; octo when a test seeded it.
func (w *world) keyOwner(key string) string {
	if a := w.ghOwner[strings.Fields(key)[len(strings.Fields(key))-1]]; a != "" {
		return a
	}
	return "octo"
}

// ghAccount is the account a gh call acts as: its GH_TOKEN's, else gh's own login.
func (w *world) ghAccount(c run.Cmd) string {
	for _, e := range c.Env {
		if v, ok := strings.CutPrefix(e, "GH_TOKEN=tok-"); ok {
			return v
		}
	}
	return w.ghLogin
}

func (w *world) grofeCmd(c run.Cmd) run.Result {
	a := c.Args
	switch {
	case len(a) == 2 && a[0] == "machine" && a[1] == "ls":
		var b strings.Builder
		for n, t := range w.grofe {
			fmt.Fprintf(&b, "%s\t%s\tmain\tssh\ttrue\n", n, t)
		}
		return run.Result{Stdout: b.String()}
	case len(a) == 4 && a[1] == "add":
		if w.fail["grofe-add"] {
			return run.Result{Code: 1, Stderr: "grofe-add failed"}
		}
		w.grofe[a[2]] = a[3]
		w.add("grofe machine add " + a[2] + " " + a[3])
		return run.Result{}
	case len(a) == 3 && a[1] == "rm":
		delete(w.grofe, a[2])
		w.add("grofe machine rm " + a[2])
		return run.Result{}
	}
	return run.Result{Code: 2, Stderr: "fake grofe: unexpected " + strings.Join(a, " ")}
}

// rbw models the parts of rbw break-glass storage uses; add and edit run the
// EDITOR script the way rbw does.
func (w *world) rbw(c run.Cmd) run.Result {
	a := c.Args
	switch a[0] {
	case "unlocked":
		if w.rbwLocked {
			return run.Result{Code: 1}
		}
		return run.Result{}
	case "list":
		var names []string
		for n, items := range w.rbwItems {
			for range items {
				names = append(names, n)
			}
		}
		slices.Sort(names)
		return run.Result{Stdout: strings.Join(names, "\n") + "\n"}
	case "get":
		if w.rbwLocked {
			return run.Result{Code: 1, Stderr: "locked"}
		}
		args := a[1:]
		field := ""
		if len(args) >= 2 && args[0] == "--field" {
			field, args = args[1], args[2:]
		}
		if len(args) != 1 || len(w.rbwItems[args[0]]) != 1 {
			return run.Result{Code: 1, Stderr: "no such entry"}
		}
		pw, notes, _ := strings.Cut(w.rbwItems[a[len(a)-1]][0], "\n")
		if field == "notes" {
			return run.Result{Stdout: notes}
		}
		return run.Result{Stdout: pw + "\n"}
	case "add", "edit":
		var editor string
		for _, e := range c.Env {
			if v, ok := strings.CutPrefix(e, "EDITOR="); ok {
				editor = v
			}
		}
		tmp := filepath.Join(w.t.TempDir(), "edit")
		if err := exec.Command("sh", editor, tmp).Run(); err != nil {
			return run.Result{Code: 1, Stderr: err.Error()}
		}
		b, err := os.ReadFile(tmp)
		if err != nil {
			return run.Result{Code: 1}
		}
		w.add("rbw-" + a[0] + " " + a[1])
		if a[0] == "add" {
			w.rbwItems[a[1]] = append(w.rbwItems[a[1]], string(b))
		} else if len(w.rbwItems[a[1]]) == 1 {
			w.rbwItems[a[1]][0] = string(b)
		}
		return run.Result{}
	}
	return run.Result{Code: 2}
}
