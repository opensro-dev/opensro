/*
===========================================================================

token_test.go - the workload token against the Agent's own verifier

TestAgentVerifierAcceptsToken is the contract with upstream: it verifies
the minted token with internal/security/workload, the code the Agent
runs, so an upstream change to the claims or the key handling fails here
before any deploy.

===========================================================================
*/
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/security/workload"
)

const testShard = "global-official"

/*
================
testKey
================
*/
func testKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

/*
================
decodeSegment
================
*/
func decodeSegment(t *testing.T, segment string, into any) {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		t.Fatalf("segment is not base64url: %v", err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("segment is not JSON: %v", err)
	}
}

/*
================
TestMintTokenClaims
================
*/
func TestMintTokenClaims(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	token := mintToken(testKey(t), "kid-1", "http://127.0.0.1:8080", "silkroad", testShard, now)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts, want 3", len(parts))
	}
	var head map[string]string
	decodeSegment(t, parts[0], &head)
	if head["alg"] != "EdDSA" || head["kid"] != "kid-1" || head["typ"] != "JWT" {
		t.Fatalf("header = %v", head)
	}
	var claims map[string]any
	decodeSegment(t, parts[1], &claims)
	want := map[string]any{
		"iss":                 "http://127.0.0.1:8080",
		"aud":                 "sro-agent",
		"iat":                 float64(now.Unix()),
		"exp":                 float64(now.Unix() + 7200),
		"nomad_namespace":     "silkroad",
		"nomad_job_id":        "sro-gameworld-global-official",
		"nomad_allocation_id": "silkroad",
		"nomad_task":          "gameworld",
	}
	if len(claims) != len(want) {
		t.Fatalf("claims = %v, want exactly %v", claims, want)
	}
	for key, value := range want {
		if claims[key] != value {
			t.Fatalf("claim %s = %v, want %v", key, claims[key], value)
		}
	}
}

/*
================
TestAgentVerifierAcceptsToken
================
*/
func TestAgentVerifierAcceptsToken(t *testing.T) {
	key := testKey(t)
	server := httptest.NewUnstartedServer(jwksHandler(key.Public().(ed25519.PublicKey), "kid-1"))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server.Listener = listener
	server.Start()
	defer server.Close()

	verifier, err := workload.NewNomadWorkloadIdentityVerifier(server.URL, server.URL+"/jwks.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	token := mintToken(key, "kid-1", server.URL, "silkroad", testShard, time.Now())
	claims, err := verifier.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Agent verifier refused the token: %v", err)
	}
	if claims.JobID != "sro-gameworld-global-official" || claims.Task != "gameworld" || claims.Namespace != "silkroad" {
		t.Fatalf("claims = %+v", claims)
	}
}
