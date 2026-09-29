package agentapi

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/security/auth"
)

func TestPrivateRoutesRequireAgentSessionForOwnedShard(t *testing.T) {
	api, authority := newTestAPI(t)
	handler := api.Handler()
	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/character/list"},
		{http.MethodPost, "/character/name-overlap"},
		{http.MethodPost, "/character/create"},
		{http.MethodPost, "/character/delete-action"},
		{http.MethodPost, "/character/enter-area"},
		{http.MethodPost, "/character/leave-area"},
		{http.MethodPost, "/agent/packet"},
		{http.MethodPost, "/auth/enterworld-token"},
		{http.MethodPost, "/auth/transport-token"},
	}
	foreign, err := auth.MintAgentSession(
		testAgentKeyID,
		testAgentPrivateKey,
		testAccount,
		"foreign-shard",
		api.now().Add(time.Hour),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range routes {
		for _, authorization := range []string{
			"",
			"Bearer forged",
			"Bearer " + foreign,
		} {
			request := httptest.NewRequest(
				route.method,
				route.path,
				strings.NewReader(`{}`),
			)
			declareBrowser(request)
			if authorization != "" {
				request.Header.Set("Authorization", authorization)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf(
					"%s %s with %q = %d, want 401",
					route.method,
					route.path,
					authorization,
					recorder.Code,
				)
			}
		}
	}
	if got := len(authority.Characters().CharactersForDivision(testDivision)); got != 0 {
		t.Fatalf("unauthorized calls created %d character(s)", got)
	}
}

func TestRequestDivisionEchoCannotRedirectWorker(t *testing.T) {
	api, authority := newTestAPI(t)
	handler := authenticatedHandler(t, api, testAccount)
	body := createBody("OwnedHero")
	body["divisionId"] = "foreign-shard"
	created := postJSON(t, handler, "/character/create", body)
	if created["nativeResult"] != float64(1) {
		t.Fatalf("create = %v", created)
	}
	if got := len(authority.Characters().CharactersForDivision("foreign-shard")); got != 0 {
		t.Fatalf("request echo created %d foreign character(s)", got)
	}
	if got := len(authority.Characters().CharactersForDivision(testDivision)); got != 1 {
		t.Fatalf("worker shard has %d character(s), want 1", got)
	}
}

func TestAccountOwnershipIsolation(t *testing.T) {
	api, authority := newTestAPI(t)
	alice := authenticatedHandler(t, api, "alice")
	bob := authenticatedHandler(t, api, "bob")

	postJSON(t, alice, "/character/create", createBody("AliceHero"))
	stored := authority.Characters().CharactersForDivision(testDivision)
	if len(stored) != 1 || stored[0].AccountID != "alice" {
		t.Fatalf("stored owner = %+v", stored)
	}

	var bobList map[string]any
	getJSON(t, bob, "/character/list", &bobList)
	if got := len(bobList["characters"].([]any)); got != 0 {
		t.Fatalf("Bob can list Alice's character: %v", bobList)
	}
	deleted := postJSON(t, bob, "/character/delete-action", map[string]any{
		"action":        3,
		"characterName": "AliceHero",
	})
	if deleted["nativeErrorCode"] != float64(errCodeUnknownID) {
		t.Fatalf("Bob delete Alice = %v", deleted)
	}
	minted := postJSON(t, bob, "/auth/enterworld-token", map[string]any{
		"characterName": "AliceHero",
	})
	if minted["ok"] != false || minted["code"] != "UNKNOWN_CHARACTER" {
		t.Fatalf("Bob EnterWorld mint = %v", minted)
	}
}

func TestEnterWorldTokenBindsShardCharacterAndTTL(t *testing.T) {
	api, _ := newTestAPI(t)
	handler := authenticatedHandler(t, api, testAccount)
	postJSON(t, handler, "/character/create", createBody("TokenHero"))

	minted := postJSON(t, handler, "/auth/enterworld-token", map[string]any{
		"characterName": "TokenHero",
	})
	token, _ := minted["token"].(string)
	if minted["ok"] != true || !strings.HasPrefix(token, "SEA3.") {
		t.Fatalf("mint = %v", minted)
	}
	now := api.now()
	if err := auth.Verify(
		[]byte(testEnterWorldSecret),
		token,
		testDivision,
		"TokenHero",
		now,
	); err != nil {
		t.Fatalf("verify minted token: %v", err)
	}
	if err := auth.Verify(
		[]byte(testEnterWorldSecret),
		token,
		"foreign-shard",
		"TokenHero",
		now,
	); err == nil {
		t.Fatal("token verified for foreign shard")
	}
	if err := auth.Verify(
		[]byte(testEnterWorldSecret),
		token,
		testDivision,
		"TokenHero",
		now.Add(enterWorldTokenTTL+time.Second),
	); err == nil {
		t.Fatal("token verified after expiry")
	}
}

func TestTransportAdmissionTokenBindsAuthenticatedAccountShardAndTTL(t *testing.T) {
	api, _ := newTestAPI(t)
	handler := authenticatedHandler(t, api, "alice")

	minted := postJSON(t, handler, "/auth/transport-token", map[string]any{})
	token, _ := minted["token"].(string)
	if minted["ok"] != true || !strings.HasPrefix(token, "STA1.") {
		t.Fatalf("mint = %v", minted)
	}
	claims, err := auth.VerifyTransportAdmission(
		[]byte(testEnterWorldSecret),
		token,
		api.now(),
	)
	if err != nil {
		t.Fatalf("verify minted transport admission: %v", err)
	}
	if claims.AccountID != "alice" || claims.ShardID != testDivision {
		t.Fatalf("claims = %+v", claims)
	}
	if _, err := auth.VerifyTransportAdmission(
		[]byte(testEnterWorldSecret),
		token,
		api.now().Add(transportAdmissionTokenTTL+time.Second),
	); !errors.Is(err, auth.ErrTransportAdmissionExpired) {
		t.Fatalf("expired token error = %v", err)
	}
}

func TestCORSIsExactAndBodiesAreBounded(t *testing.T) {
	api, authority := newTestAPI(t)
	handler := api.Handler()

	allowed := httptest.NewRequest(
		http.MethodOptions,
		"/character/list",
		nil,
	)
	allowed.Header.Set("Origin", "http://localhost:4173")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, allowed)
	if recorder.Code != http.StatusNoContent ||
		recorder.Header().Get("Access-Control-Allow-Origin") !=
			"http://localhost:4173" {
		t.Fatalf("allowed preflight = %d %v", recorder.Code, recorder.Header())
	}

	hostile := httptest.NewRequest(
		http.MethodOptions,
		"/character/create",
		nil,
	)
	hostile.Header.Set("Origin", "https://attacker.example")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, hostile)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("hostile preflight = %d, want 403", recorder.Code)
	}

	authenticated := authenticatedHandler(t, api, testAccount)
	oversized := bytes.Repeat([]byte{'x'}, int(maxBodyBytes)+1)
	request := httptest.NewRequest(
		http.MethodPost,
		"/character/create",
		bytes.NewReader(oversized),
	)
	recorder = httptest.NewRecorder()
	authenticated.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK ||
		!strings.Contains(recorder.Body.String(), `"nativeResult":0`) {
		t.Fatalf(
			"oversized body did not receive native refusal: %d %s",
			recorder.Code,
			recorder.Body.String(),
		)
	}
	if got := len(authority.Characters().CharactersForDivision(testDivision)); got != 0 {
		t.Fatalf("oversized body created %d character(s)", got)
	}
}

func TestGuildMarkEndpointRefusesTraversalSymlinkAndOversize(
	t *testing.T,
) {
	api, _ := newTestAPI(t)
	marksDir := t.TempDir()
	api.marksDir = marksDir
	crest := []byte{0x43, 0x52, 0x42, 0x00, 0xde, 0xad}
	if err := os.WriteFile(
		filepath.Join(marksDir, "G1_3_7.crb"),
		crest,
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	get := func(path string) *httptest.ResponseRecorder {
		result := httptest.NewRecorder()
		api.Handler().ServeHTTP(
			result,
			httptest.NewRequest(http.MethodGet, path, nil),
		)
		return result
	}
	if result := get("/marks/G1_3_7.crb"); result.Code != http.StatusOK ||
		!bytes.Equal(result.Body.Bytes(), crest) {
		t.Fatalf("crest = %d %x", result.Code, result.Body.Bytes())
	}
	for _, path := range []string{
		"/marks/..%2fG1_3_7.crb",
		"/marks/G1_3_7.crb/extra",
		"/marks/X1_3_7.crb",
	} {
		if result := get(path); result.Code == http.StatusOK {
			t.Fatalf("unsafe crest path %q served", path)
		}
	}

	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(marksDir, "G2_3_7.crb")
	if err := os.Symlink(outside, link); err == nil {
		if code := get("/marks/G2_3_7.crb").Code; code != http.StatusNotFound {
			t.Fatalf("symlink crest = %d, want 404", code)
		}
	}
	if err := os.WriteFile(
		filepath.Join(marksDir, "G3_3_7.crb"),
		make([]byte, maxGuildMarkBytes+1),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if code := get("/marks/G3_3_7.crb").Code; code != http.StatusNotFound {
		t.Fatalf("oversized crest = %d, want 404", code)
	}
}
