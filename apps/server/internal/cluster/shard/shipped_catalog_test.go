/*
===========================================================================

shipped_catalog_test.go - the shard catalogue the module ships is usable

config/shards.json is what a development cluster and the operator tools
load by default; this test reads the shipped file itself, not a fixture.

===========================================================================
*/
package shard

import (
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

/*
================
TestShippedCatalog

Every enabled shard has its own control and transport endpoint, loopback
shards speak plain http, clients reach each shard through a same-origin
route, and the default shard exists and is enabled.
================
*/
func TestShippedCatalog(t *testing.T) {
	// A test runs in its package's folder. runtime.Caller is no way to the
	// module: the gate compiles with -trimpath, which leaves no source
	// folder in the binary (scripts/lib/goEnvironment.mjs).
	catalogPath := filepath.Join("..", "..", "..", "config", "shards.json")
	catalog, err := Load(catalogPath)
	if err != nil {
		t.Fatalf("load shipped catalog: %v", err)
	}

	definitions := catalog.Definitions()
	if len(definitions) < 1 {
		t.Fatal("shipped catalog is empty")
	}

	seenControlPorts := map[string]string{}
	seenTransportPorts := map[string]string{}
	for _, definition := range definitions {
		if !definition.Enabled {
			continue
		}
		if prior, exists := seenControlPorts[definition.ControlURL]; exists {
			t.Fatalf("duplicate controlUrl %q on shards %q and %q", definition.ControlURL, prior, definition.ID)
		}
		seenControlPorts[definition.ControlURL] = definition.ID
		if prior, exists := seenTransportPorts[definition.TransportURL]; exists {
			t.Fatalf("duplicate transportUrl %q on shards %q and %q", definition.TransportURL, prior, definition.ID)
		}
		seenTransportPorts[definition.TransportURL] = definition.ID
		endpoint, err := url.Parse(definition.TransportURL)
		if err != nil {
			t.Fatalf("parse transportUrl for %q: %v", definition.ID, err)
		}
		if ip := net.ParseIP(endpoint.Hostname()); ip != nil &&
			ip.IsLoopback() &&
			endpoint.Scheme != "http" {
			t.Fatalf(
				"loopback shard %q transportUrl = %q; want http base for WS and cert-hash endpoint",
				definition.ID,
				definition.TransportURL,
			)
		}
		// Development clients reach every shard through their own origin
		// (localhost, a LAN address, HTTPS); the web edge routes it.
		if !strings.HasPrefix(definition.PublicTransportURL, "/") {
			t.Fatalf(
				"shipped shard %q publicTransportUrl = %q; want a same-origin edge route",
				definition.ID,
				definition.PublicTransportURL,
			)
		}
	}

	defaultShard := catalog.Default()
	if defaultShard.ID == "" {
		t.Fatal("shipped catalog has no default shard")
	}
	if !defaultShard.Enabled {
		t.Fatalf("default shard %q must be enabled", defaultShard.ID)
	}
}
