/*
===========================================================================

provisioning_test.go - the provisioning API over the real account store

===========================================================================
*/
package provisioning

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/security/auth"
)

const testToken = "0123456789abcdef0123456789abcdef-test"

/*
================
startAPI
================
*/
func startAPI(t *testing.T) *httptest.Server {
	t.Helper()
	accounts, err := auth.OpenAccounts(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = accounts.Close() })
	server, err := New(accounts, testToken)
	if err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(server.Handler())
	t.Cleanup(api.Close)
	return api
}

/*
================
call

Sends one request and returns the status and decoded JSON body (nil when
the body is empty).
================
*/
func call(t *testing.T, api *httptest.Server, token, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequest(method, api.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := api.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(response.Body)
	if len(payload) == 0 {
		return response.StatusCode, nil
	}
	var document map[string]any
	if err := json.Unmarshal(payload, &document); err != nil {
		t.Fatalf("%s %s: body %q is not JSON", method, path, payload)
	}
	return response.StatusCode, document
}

func TestProvisioningRejectsMissingAndWrongTokens(t *testing.T) {
	api := startAPI(t)
	for _, token := range []string{"", "wrong-token-wrong-token-wrong-token"} {
		status, body := call(t, api, token, "GET", "/v1/accounts/anyone", nil)
		if status != http.StatusUnauthorized || body["code"] != "UNAUTHORIZED" {
			t.Fatalf("token %q = %d %v", token, status, body)
		}
	}
	if _, err := New(nil, testToken); err == nil {
		t.Fatal("accepted a nil authority")
	}
	accounts, _ := auth.OpenAccounts(filepath.Join(t.TempDir(), "a.db"))
	defer accounts.Close()
	if _, err := New(accounts, "short"); err == nil {
		t.Fatal("accepted a short token")
	}
}

func TestProvisioningAccountLifecycle(t *testing.T) {
	api := startAPI(t)
	status, body := call(t, api, testToken, "POST", "/v1/accounts", map[string]string{"id": "hunter", "password": "correct horse"})
	if status != http.StatusCreated || body["id"] != "hunter" || body["disabled"] != false {
		t.Fatalf("create = %d %v", status, body)
	}
	status, body = call(t, api, testToken, "POST", "/v1/accounts", map[string]string{"id": "HUNTER", "password": "correct horse"})
	if status != http.StatusConflict || body["code"] != "ACCOUNT_EXISTS" {
		t.Fatalf("duplicate = %d %v", status, body)
	}
	status, body = call(t, api, testToken, "POST", "/v1/accounts/hunter/verify", map[string]string{"password": "correct horse"})
	if status != http.StatusOK || body["valid"] != true {
		t.Fatalf("verify = %d %v", status, body)
	}
	status, body = call(t, api, testToken, "POST", "/v1/accounts/ghost/verify", map[string]string{"password": "correct horse"})
	if status != http.StatusOK || body["valid"] != false {
		t.Fatalf("verify missing = %d %v (must not reveal absence)", status, body)
	}
	if status, _ = call(t, api, testToken, "PUT", "/v1/accounts/hunter/password", map[string]string{"password": "new password"}); status != http.StatusNoContent {
		t.Fatalf("password = %d", status)
	}
	if _, body = call(t, api, testToken, "POST", "/v1/accounts/hunter/verify", map[string]string{"password": "new password"}); body["valid"] != true {
		t.Fatal("new password refused")
	}
	if status, _ = call(t, api, testToken, "PUT", "/v1/accounts/hunter/disabled", map[string]bool{"disabled": true}); status != http.StatusNoContent {
		t.Fatalf("disable = %d", status)
	}
	status, body = call(t, api, testToken, "GET", "/v1/accounts/hunter", nil)
	if status != http.StatusOK || body["disabled"] != true {
		t.Fatalf("lookup = %d %v", status, body)
	}
	if _, body = call(t, api, testToken, "POST", "/v1/accounts/hunter/verify", map[string]string{"password": "new password"}); body["valid"] != false {
		t.Fatal("disabled account verified")
	}
}

func TestProvisioningRejectsBadInput(t *testing.T) {
	api := startAPI(t)
	cases := []struct {
		method, path string
		body         any
		status       int
		code         string
	}{
		{"POST", "/v1/accounts", map[string]string{"id": "x", "password": "short"}, http.StatusBadRequest, "INVALID_PASSWORD"},
		{"POST", "/v1/accounts", map[string]string{"id": "", "password": "long enough"}, http.StatusBadRequest, "INVALID_ID"},
		{"POST", "/v1/accounts", map[string]string{"id": "x", "password": "long enough", "role": "gm"}, http.StatusBadRequest, "INVALID_REQUEST"},
		{"PUT", "/v1/accounts/ghost/password", map[string]string{"password": "long enough"}, http.StatusNotFound, "ACCOUNT_NOT_FOUND"},
		{"PUT", "/v1/accounts/ghost/disabled", map[string]any{}, http.StatusBadRequest, "INVALID_REQUEST"},
		{"GET", "/v1/accounts/ghost", nil, http.StatusNotFound, "ACCOUNT_NOT_FOUND"},
	}
	for _, c := range cases {
		status, body := call(t, api, testToken, c.method, c.path, c.body)
		if status != c.status || body["code"] != c.code {
			t.Fatalf("%s %s %v = %d %v, want %d %s", c.method, c.path, c.body, status, body, c.status, c.code)
		}
	}
}
