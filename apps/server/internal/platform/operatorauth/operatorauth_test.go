/*
===========================================================================

operatorauth_test.go - only the local console with the token is let in

===========================================================================
*/
package operatorauth

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

/*
================
TestGuardAdmitsOnlyTheLocalConsole

Each refusal changes one thing about an admitted request.
================
*/
func TestGuardAdmitsOnlyTheLocalConsole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	token := "0123456789abcdef0123456789abcdef"
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	guard, found, err := Load(path, "test")
	if err != nil || !found {
		t.Fatalf("load: %v %v", found, err)
	}
	request := func(change func(*http.Request)) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8791/x", nil)
		r.RemoteAddr = "127.0.0.1:40000"
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-SRO-Local-Diagnostics", "1")
		if change != nil {
			change(r)
		}
		return r
	}
	if !guard.Allows(request(nil)) {
		t.Fatal("the local console was refused")
	}
	for name, change := range map[string]func(*http.Request){
		"remote peer":    func(r *http.Request) { r.RemoteAddr = "203.0.113.5:40000" },
		"public host":    func(r *http.Request) { r.Host = "opensro.online" },
		"no diagnostics": func(r *http.Request) { r.Header.Del("X-SRO-Local-Diagnostics") },
		"browser origin": func(r *http.Request) { r.Header.Set("Origin", "http://127.0.0.1:5190") },
		"forwarded":      func(r *http.Request) { r.Header.Set("X-Forwarded-For", "203.0.113.5") },
		"wrong token":    func(r *http.Request) { r.Header.Set("Authorization", "Bearer nope") },
	} {
		if guard.Allows(request(change)) {
			t.Fatalf("%s was admitted", name)
		}
	}
	if _, found, err := Load(filepath.Join(t.TempDir(), "absent"), "test"); found || err != nil {
		t.Fatalf("an absent token file: found %v, %v", found, err)
	}
	short := filepath.Join(t.TempDir(), "short")
	_ = os.WriteFile(short, []byte("short"), 0o600)
	if _, _, err := Load(short, "test"); err == nil {
		t.Fatal("a short token was accepted")
	}
}
