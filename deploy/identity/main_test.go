/*
===========================================================================

main_test.go - the key file and the published JWKS

===========================================================================
*/
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

/*
================
TestKeyIsCreatedOnceAndKept
================
*/
func TestKeyIsCreatedOnceAndKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key.json")
	first, firstKid, err := loadOrCreateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	second, secondKid, err := loadOrCreateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if firstKid != secondKid || !first.Equal(second) {
		t.Fatalf("second load gave another key: %s vs %s", firstKid, secondKid)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode = %v, want 0600", info.Mode().Perm())
	}
}

/*
================
TestJWKSServesThePublicKey
================
*/
func TestJWKSServesThePublicKey(t *testing.T) {
	key := testKey(t)
	public := key.Public().(ed25519.PublicKey)
	recorder := httptest.NewRecorder()
	jwksHandler(public, "kid-1").ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/jwks.json", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var document struct {
		Keys []map[string]string `json:"keys"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Keys) != 1 {
		t.Fatalf("keys = %v", document.Keys)
	}
	jwk := document.Keys[0]
	if jwk["kty"] != "OKP" || jwk["crv"] != "Ed25519" || jwk["kid"] != "kid-1" || jwk["alg"] != "EdDSA" || jwk["use"] != "sig" {
		t.Fatalf("jwk = %v", jwk)
	}
	x, err := base64.RawURLEncoding.DecodeString(jwk["x"])
	if err != nil || !public.Equal(ed25519.PublicKey(x)) {
		t.Fatalf("x does not decode to the public key")
	}
}

/*
================
TestWriteTokensIsAtomicPerShard
================
*/
func TestWriteTokensIsAtomicPerShard(t *testing.T) {
	dir := t.TempDir()
	config := tokenConfig{key: testKey(t), kid: "kid-1", issuer: "http://127.0.0.1:8080", namespace: "silkroad", outDir: dir, shards: []string{"global-official", "test"}}
	if err := writeTokens(config); err != nil {
		t.Fatal(err)
	}
	for _, shard := range config.shards {
		info, err := os.Stat(filepath.Join(dir, "gameworld-"+shard+".jwt"))
		if err != nil {
			t.Fatalf("shard %s: %v", shard, err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Fatalf("shard %s mode = %v", shard, info.Mode().Perm())
		}
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if len(leftovers) != 0 {
		t.Fatalf("temporary files left behind: %v", leftovers)
	}
}
