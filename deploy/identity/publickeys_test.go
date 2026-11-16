/*
===========================================================================

publickeys_test.go - the GameWorld's public session key ring

===========================================================================
*/
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	clusterprovision "opensro.online/server/internal/cluster/provision"
	"opensro.online/server/internal/security/auth"
)

/*
================
TestDerivePublicIsIdempotent
================
*/
func TestDerivePublicIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	ring, err := clusterprovision.EnsureIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "agent-session-public-keys.json")
	changed, err := derivePublic(ring.Path, out)
	if err != nil || !changed {
		t.Fatalf("first derive: changed=%v err=%v", changed, err)
	}
	first, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	changed, err = derivePublic(ring.Path, out)
	if err != nil || changed {
		t.Fatalf("second derive: changed=%v err=%v", changed, err)
	}
	private, err := os.ReadFile(ring.Path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := auth.PublicAgentSessionKeyRing(private)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, want) {
		t.Fatalf("derived ring differs from auth.PublicAgentSessionKeyRing")
	}
}
