package machines

import (
	"bytes"
	"testing"
)

func TestList(t *testing.T) {
	t.Setenv("HOSTNAME", "lap")
	home := t.TempDir()
	var b bytes.Buffer
	if err := List(home, &b); err != nil || b.String() != "no machines\n" {
		t.Fatalf("empty: %q %v", b.String(), err)
	}
	if err := Save(home, []Machine{gpu(), acme()}); err != nil {
		t.Fatal(err)
	}
	b.Reset()
	if err := List(home, &b); err != nil {
		t.Fatal(err)
	}
	want := "acme-dev         work/acme            dev@dev.acme.example         1 repos\n" +
		"acme-gpu         work/acme            ops@10.0.0.7:2222            0 repos\n"
	if b.String() != want {
		t.Fatalf("got:\n%q\nwant:\n%q", b.String(), want)
	}
}
