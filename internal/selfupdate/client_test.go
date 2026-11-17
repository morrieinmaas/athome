package selfupdate

import (
	"net/http"
	"testing"
)

func TestClientHasTimeouts(t *testing.T) {
	if client.Timeout <= 0 {
		t.Error("client has no overall timeout")
	}
	tr, ok := client.Transport.(*http.Transport)
	if !ok || tr.ResponseHeaderTimeout <= 0 || tr.DialContext == nil {
		t.Errorf("transport has no connect or header timeout: %+v", client.Transport)
	}
}
