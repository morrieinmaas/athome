package machines

import "testing"

func TestDeleteByTitleKeepsOtherMachinesKeys(t *testing.T) {
	w, d, _ := newWorld(t)
	w.ghKeys["user/keys"] = []fakeGHKey{
		{"1", "ssh-ed25519 AAAAA", "athome-foo"},
		{"2", "ssh-ed25519 AAAAB", "athome-foo-sign"}, // machine foo-sign's authentication key
	}
	w.ghKeys["user/ssh_signing_keys"] = []fakeGHKey{
		{"3", "ssh-ed25519 AAAAA", "athome-foo-sign"},
		{"4", "ssh-ed25519 AAAAB", "athome-foo-sign-sign"},
		{"5", "ssh-ed25519 AAAAC", "athome-foo"}, // not a title rm foo owns on this endpoint
	}
	n, err := d.gh(Machine{}).deleteByTitle(t.Context(), "foo")
	if err != nil || n != 2 {
		t.Fatalf("deleted %d: %v", n, err)
	}
	if got := w.ghKeys["user/keys"]; len(got) != 1 || got[0].id != "2" {
		t.Errorf("user/keys = %+v", got)
	}
	if got := w.ghKeys["user/ssh_signing_keys"]; len(got) != 2 || got[0].id != "4" || got[1].id != "5" {
		t.Errorf("user/ssh_signing_keys = %+v", got)
	}
}
