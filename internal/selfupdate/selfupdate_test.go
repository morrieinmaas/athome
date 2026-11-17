package selfupdate_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/selfupdate"
)

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{"0.5.2": "v0.5.2", "v0.5.2": "v0.5.2", " v1.10.0 ": "v1.10.0"} {
		got, ok := selfupdate.Normalize(in)
		if !ok || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"v0.5", "latest", "dev", "", "v1.2.3-rc1", "v1.2.x", "1.2.3.4"} {
		if got, ok := selfupdate.Normalize(in); ok {
			t.Errorf("Normalize(%q) = %q, want not ok", in, got)
		}
	}
}

func TestCompare(t *testing.T) {
	if selfupdate.Compare("0.10.0", "v0.9.9") <= 0 || selfupdate.Compare("v0.5.2", "0.5.2") != 0 || selfupdate.Compare("0.5.0", "0.5.2") >= 0 {
		t.Fatal("Compare is not numeric per component")
	}
}

func TestHighest(t *testing.T) {
	got := selfupdate.Highest([]string{"v0.9.0", "v0.10.0", "nightly", "v0.2.0", "v1.0", "v0.10.0-rc1"})
	if got != "v0.10.0" {
		t.Fatalf("Highest = %q", got)
	}
	if selfupdate.Highest([]string{"nightly"}) != "" {
		t.Fatal("no release tag should give empty")
	}
}

type release struct {
	srv   *httptest.Server
	asset string
	hits  []string
}

func serve(t *testing.T, body, checksums string, skipAsset bool) *release {
	t.Helper()
	r := &release{asset: "athome_linux_amd64"}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		r.hits = append(r.hits, q.URL.Path)
		switch q.URL.Path {
		case "/v0.5.2/athome_linux_amd64":
			if skipAsset {
				http.NotFound(w, q)
				return
			}
			fmt.Fprint(w, body)
		case "/v0.5.2/checksums.txt":
			fmt.Fprint(w, checksums)
		default:
			http.NotFound(w, q)
		}
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func sum(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }

func dest(t *testing.T) string {
	d := filepath.Join(t.TempDir(), "bin", "athome")
	if err := os.MkdirAll(filepath.Dir(d), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	return d
}

func noPart(t *testing.T, d string) {
	t.Helper()
	es, _ := os.ReadDir(filepath.Dir(d))
	for _, e := range es {
		if strings.HasPrefix(e.Name(), ".athome.") {
			t.Errorf("left behind %s", e.Name())
		}
	}
}

func TestInstallOK(t *testing.T) {
	r := serve(t, "new", sum("new")+"  athome_linux_amd64\n", false)
	d := dest(t)
	if err := selfupdate.Install(context.Background(), r.srv.URL, "v0.5.2", "linux", "amd64", d); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(d)
	fi, _ := os.Stat(d)
	if string(b) != "new" || fi.Mode().Perm() != 0o755 {
		t.Fatalf("got %q %v", b, fi.Mode())
	}
	noPart(t, d)
}

func TestInstallFailuresKeepOldBinary(t *testing.T) {
	cases := map[string]*release{
		"wrong checksum":   serve(t, "new", sum("other")+"  athome_linux_amd64\n", false),
		"no checksum line": serve(t, "new", sum("new")+"  athome_darwin_arm64\n", false),
		"404 asset":        serve(t, "new", sum("new")+"  athome_linux_amd64\n", true),
	}
	for name, r := range cases {
		t.Run(name, func(t *testing.T) {
			d := dest(t)
			err := selfupdate.Install(context.Background(), r.srv.URL, "v0.5.2", "linux", "amd64", d)
			if err == nil {
				t.Fatal("want error")
			}
			if name == "404 asset" && !strings.Contains(err.Error(), "/v0.5.2/athome_linux_amd64") {
				t.Errorf("error does not name the asset URL: %v", err)
			}
			if b, _ := os.ReadFile(d); string(b) != "old" {
				t.Fatalf("binary changed: %q", b)
			}
			noPart(t, d)
		})
	}
}

func TestInstallFileBaseAndUnwritable(t *testing.T) {
	rel := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rel, "v0.5.2"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(rel, "v0.5.2", "athome_linux_amd64"), []byte("new"), 0o644)
	os.WriteFile(filepath.Join(rel, "v0.5.2", "checksums.txt"), []byte(sum("new")+" *athome_linux_amd64\n"), 0o644)
	d := dest(t)
	if err := selfupdate.Install(context.Background(), "file://"+rel, "v0.5.2", "linux", "amd64", d); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(d); string(b) != "new" {
		t.Fatalf("got %q", b)
	}
	if os.Getuid() == 0 {
		return
	}
	d = dest(t)
	os.Chmod(filepath.Dir(d), 0o555)
	t.Cleanup(func() { os.Chmod(filepath.Dir(d), 0o755) })
	if err := selfupdate.Install(context.Background(), "file://"+rel, "v0.5.2", "linux", "amd64", d); err == nil {
		t.Fatal("want error for unwritable dir")
	}
	if b, _ := os.ReadFile(d); string(b) != "old" {
		t.Fatalf("binary changed: %q", b)
	}
}

func TestInstallTagWithoutAssetsNamesAssetURL(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	d := dest(t)
	err := selfupdate.Install(context.Background(), srv.URL, "v0.5.2", "linux", "amd64", d)
	if err == nil || !strings.Contains(err.Error(), "/v0.5.2/athome_linux_amd64") {
		t.Fatalf("error does not name the asset URL: %v", err)
	}
	noPart(t, d)
}
