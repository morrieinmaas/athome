// Package selfupdate finds release tags and replaces the athome binary from a release.
package selfupdate

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const DefaultBase = "https://github.com/morrieinmaas/athome/releases/download"

// Parse reads X.Y.Z or vX.Y.Z, nothing else.
func Parse(s string) ([3]int, bool) {
	var v [3]int
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(s), "v"), ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p != strconv.Itoa(n) {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

// Normalize returns s as vX.Y.Z.
func Normalize(s string) (string, bool) {
	v, ok := Parse(s)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("v%d.%d.%d", v[0], v[1], v[2]), true
}

// Compare orders two release versions; both must parse.
func Compare(a, b string) int {
	x, _ := Parse(a)
	y, _ := Parse(b)
	for i := range x {
		if x[i] != y[i] {
			if x[i] < y[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// Highest returns the highest vX.Y.Z tag, or "" when there is none.
func Highest(tags []string) string {
	best := ""
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if !strings.HasPrefix(t, "v") {
			continue
		}
		if _, ok := Parse(t); !ok {
			continue
		}
		if best == "" || Compare(t, best) > 0 {
			best = t
		}
	}
	return best
}

// The client bounds every request: connect 10 s, response headers 30 s, and
// 5 min overall, which covers reading the binary body.
var client = func() *http.Client {
	t := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	t.RegisterProtocol("file", http.NewFileTransport(http.Dir("/")))
	return &http.Client{Transport: t, Timeout: 5 * time.Minute}
}()

func get(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot download %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("cannot download %s: %s", url, resp.Status)
	}
	return resp.Body, nil
}

// Install downloads athome_<goos>_<goarch> of tag from base, checks it against
// checksums.txt and renames it over dest. On any error dest is untouched and no
// temporary file is left.
func Install(ctx context.Context, base, tag, goos, goarch, dest string) error {
	asset := fmt.Sprintf("athome_%s_%s", goos, goarch)
	base = strings.TrimSuffix(base, "/") + "/" + tag + "/"

	// The asset goes first, so a tag without assets fails naming the asset URL.
	body, err := get(ctx, base+asset)
	if err != nil {
		return fmt.Errorf("%w (is the release for %s published?)", err, tag)
	}
	defer body.Close()

	sumsBody, err := get(ctx, base+"checksums.txt")
	if err != nil {
		return err
	}
	sums, err := io.ReadAll(sumsBody)
	sumsBody.Close()
	if err != nil {
		return fmt.Errorf("read %schecksums.txt: %w", base, err)
	}
	want := ""
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == asset {
			want = f[0]
			break
		}
	}
	if want == "" {
		return fmt.Errorf("checksums.txt has no line for %s", asset)
	}

	part := filepath.Join(filepath.Dir(dest), fmt.Sprintf(".athome.%d", os.Getpid()))
	f, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", part, err)
	}
	done := false
	defer func() {
		if !done {
			f.Close()
			os.Remove(part)
		}
	}()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), body); err != nil {
		return fmt.Errorf("download %s: %w", asset, err)
	}
	if got := fmt.Sprintf("%x", h.Sum(nil)); got != want {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", asset, want, got)
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(part, 0o755); err != nil {
		return err
	}
	if err := os.Rename(part, dest); err != nil {
		return fmt.Errorf("cannot install %s: %w", dest, err)
	}
	done = true
	return nil
}
