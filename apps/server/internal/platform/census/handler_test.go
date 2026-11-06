/*
===========================================================================

handler_test.go - the census routes behind the operator guard

No token file answers nobody; a request without the token, from a
browser origin or through a forward is refused; an allowed read
answers the contract, and the person route names its errors.

===========================================================================
*/
package census

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testToken = "census-test-token-census-test-token-1234"

// call issues one operator request; empty token sends no credentials.
func call(handler http.Handler, route, token string) (int, map[string]any) {
	r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8791"+Path+route, nil)
	r.RemoteAddr = "127.0.0.1:50000"
	r.Header.Set("X-SRO-Local-Diagnostics", "1")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	body := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return w.Code, body
}

// handlerFor writes the token file and builds the guarded handler.
func handlerFor(t *testing.T, service *Service) http.Handler {
	t.Helper()
	tokenPath := filepath.Join(t.TempDir(), "operator-token")
	if err := os.WriteFile(tokenPath, []byte(testToken), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := Handler(service, tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

/*
================
TestHandlerWithoutTokenFileAnswersNobody
================
*/
func TestHandlerWithoutTokenFileAnswersNobody(t *testing.T) {
	handler, err := Handler(testService(nil, nil), filepath.Join(t.TempDir(), "operator-token"))
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := call(handler, "/online", testToken); code != http.StatusNotFound {
		t.Fatalf("no token file: got %d, want 404", code)
	}
}

/*
================
TestHandlerGuardAndRoutes
================
*/
func TestHandlerGuardAndRoutes(t *testing.T) {
	now := nowMs()
	spans := []Span{
		span("s1", "acct-a", "Anna", now-hourMs, 0, "ip-a", "net-a", "client-a"),
	}
	handler := handlerFor(t, testService(spans, nil))

	if code, _ := call(handler, "/online", ""); code != http.StatusForbidden {
		t.Fatalf("no credentials: got %d, want 403", code)
	}
	if code, _ := call(handler, "/online", "wrong-token-wrong-token-wrong-token"); code != http.StatusForbidden {
		t.Fatalf("wrong token: got %d, want 403", code)
	}
	code, body := call(handler, "/online", testToken)
	if code != http.StatusOK {
		t.Fatalf("online: got %d (%v), want 200", code, body)
	}
	if body["schemaVersion"] != float64(SchemaVersion) || body["people"] != float64(1) {
		t.Fatalf("online body: got %v, want schemaVersion 1 and one person", body)
	}
	if code, _ := call(handler, "/uniques", testToken); code != http.StatusOK {
		t.Fatalf("uniques: got %d, want 200", code)
	}
	if code, _ := call(handler, "/clusters", testToken); code != http.StatusOK {
		t.Fatalf("clusters: got %d, want 200", code)
	}
	if code, _ := call(handler, "/signals", testToken); code != http.StatusOK {
		t.Fatalf("signals: got %d, want 200", code)
	}
	if code, _ := call(handler, "/bogus", testToken); code != http.StatusNotFound {
		t.Fatalf("unknown route: got %d, want 404", code)
	}
	if code, _ := call(handler, "/online?window=99d", testToken); code != http.StatusBadRequest {
		t.Fatalf("bad window: got %d, want 400", code)
	}

	code, _ = call(handler, "/person", testToken)
	if code != http.StatusBadRequest {
		t.Fatalf("person without a name: got %d, want 400", code)
	}
	code, body = call(handler, "/person?character=Nobody", testToken)
	if code != http.StatusNotFound {
		t.Fatalf("unknown person: got %d, want 404", code)
	}
	if errBody, ok := body["error"].(map[string]any); !ok || errBody["code"] != "no_such_character" {
		t.Fatalf("unknown person error: got %v, want the no_such_character contract", body)
	}
	code, body = call(handler, "/person?character=Anna", testToken)
	if code != http.StatusOK || body["account"] != "acct-a" {
		t.Fatalf("person: got %d %v, want 200 for acct-a", code, body)
	}
	for _, secret := range []string{"ip-a", "net-a", "client-a"} {
		if strings.Contains(body["character"].(string), secret) {
			t.Fatalf("person answer carries %q", secret)
		}
	}

	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8791"+Path+"/online", nil)
	r.RemoteAddr = "127.0.0.1:50000"
	r.Header.Set("X-SRO-Local-Diagnostics", "1")
	r.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: got %d, want 405", w.Code)
	}
}
