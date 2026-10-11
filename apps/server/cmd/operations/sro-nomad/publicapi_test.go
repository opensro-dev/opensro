/*
===========================================================================

publicapi_test.go - the public API listener and its token reach one shard

===========================================================================
*/
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opensro.online/server/internal/agent/publicstats"
	"opensro.online/server/internal/cluster/shard"
	"opensro.online/server/internal/security/auth"
)

/*
================
TestPublicAPITokenReachesOnlyTheServingGameWorld

The token goes to the GameWorld that serves the listener, never to the
Agent and never to a shard whose listener is off; without a token no item
exists, so the template renders an empty file.
================
*/
func TestPublicAPITokenReachesOnlyTheServingGameWorld(t *testing.T) {
	token := strings.Repeat("a", auth.MinProvisioningTokenBytes)
	shards := []shardDeployment{
		{Definition: shard.Definition{ID: "global-official"}, PublicAPIAddr: publicstats.DefaultAddr},
		{Definition: shard.Definition{ID: "test"}, PublicAPIAddr: "off"},
	}
	withToken := deployment{Secrets: clusterSecrets{PublicAPIToken: token}, Shards: shards}
	variables, err := withToken.desiredVariables()
	if err != nil {
		t.Fatal(err)
	}
	if _, found := variables[0].items[publicAPITokenItem]; found {
		t.Fatal("the Agent received the public API token")
	}
	if variables[1].items[publicAPITokenItem] != token {
		t.Fatal("the serving GameWorld did not receive the public API token")
	}
	if _, found := variables[2].items[publicAPITokenItem]; found {
		t.Fatal("a GameWorld with its listener off received the public API token")
	}

	without := deployment{Shards: shards}
	variables, err = without.desiredVariables()
	if err != nil {
		t.Fatal(err)
	}
	if _, found := variables[1].items[publicAPITokenItem]; found {
		t.Fatal("an empty public API token item was rendered")
	}
	if got := without.gameVariables(shards[1])["public_api_addr"]; got != "off" {
		t.Fatalf("second shard public_api_addr = %v, want off", got)
	}
}

/*
================
TestOptionalTokenRefusesAShortToken

A missing file is no token; a present one must be long enough.
================
*/
func TestOptionalTokenRefusesAShortToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, auth.PublicAPITokenFile)
	if token, err := optionalToken(path); err != nil || token != "" {
		t.Fatalf("missing token = %q, %v", token, err)
	}
	if err := os.WriteFile(path, []byte("short\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := optionalToken(path); err == nil {
		t.Fatal("a short token was accepted")
	}
	long := strings.Repeat("b", auth.MinProvisioningTokenBytes)
	if err := os.WriteFile(path, []byte(long+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if token, err := optionalToken(path); err != nil || token != long {
		t.Fatalf("token = %q, %v", token, err)
	}
}
