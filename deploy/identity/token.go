/*
===========================================================================

token.go - the GameWorld workload token and the JWKS that verifies it

The Agent authorizes GameWorld control calls with a Nomad workload
identity token (apps/server/internal/security/workload). Without Nomad,
this service plays Nomad's part, exactly as scripts/local/run.mjs does
for `pnpm local`: one Ed25519 key, its JWKS, and a token per shard with
the same claims mintToken there writes.

===========================================================================
*/
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"
)

const (
	// tokenLifetime and tokenRefresh are scripts/local/run.mjs's
	// tokenLifetimeMs and tokenRefreshMs: a token is rewritten long
	// before it expires, and the GameWorld re-reads the file per call.
	tokenLifetime = 2 * time.Hour
	tokenRefresh  = 30 * time.Minute

	agentAudience = "sro-agent"
	gameworldTask = "gameworld"
)

/*
================
encodeSegment
================
*/
func encodeSegment(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		// Only maps of strings and numbers reach here.
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

/*
================
mintToken

The GameWorld's control token for one shard. The allocation id carries
the namespace: the verifier only requires it to be present, and there is
no Nomad allocation to name.
================
*/
func mintToken(key ed25519.PrivateKey, kid, issuer, namespace, shard string, now time.Time) string {
	head := encodeSegment(map[string]string{"alg": "EdDSA", "kid": kid, "typ": "JWT"})
	body := encodeSegment(map[string]any{
		"iss":                 issuer,
		"aud":                 agentAudience,
		"iat":                 now.Unix(),
		"exp":                 now.Add(tokenLifetime).Unix(),
		"nomad_namespace":     namespace,
		"nomad_job_id":        "sro-gameworld-" + shard,
		"nomad_allocation_id": namespace,
		"nomad_task":          gameworldTask,
	})
	signature := ed25519.Sign(key, []byte(head+"."+body))
	return head + "." + body + "." + base64.RawURLEncoding.EncodeToString(signature)
}

/*
================
jwksHandler

Serves the one public key the Agent verifies tokens against.
================
*/
func jwksHandler(public ed25519.PublicKey, kid string) http.Handler {
	document, err := json.Marshal(map[string]any{
		"keys": []map[string]string{{
			"kty": "OKP",
			"crv": "Ed25519",
			"x":   base64.RawURLEncoding.EncodeToString(public),
			"kid": kid,
			"alg": "EdDSA",
			"use": "sig",
		}},
	})
	if err != nil {
		panic(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(document)
	})
	return mux
}
