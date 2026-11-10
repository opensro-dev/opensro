/*
===========================================================================

register_shard_policy_test.go - the development shard policy reads the
shipped catalogue

===========================================================================
*/
package enterworld

import (
	"path/filepath"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestDevShardPolicyFromCatalog

The policy built from config/shards.json defaults to the global-official
division and lets a character resolve to the test division.
================
*/
func TestDevShardPolicyFromCatalog(t *testing.T) {
	// A test runs in its package's folder. runtime.Caller is no way to the
	// module: the gate compiles with -trimpath, which leaves no source
	// folder in the binary (scripts/lib/goEnvironment.mjs).
	catalogPath, err := filepath.Abs(filepath.Join("..", "..", "..", "config", "shards.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SRO_SHARD_CATALOG_PATH", catalogPath)

	policy := devShardPolicyFromCatalog()
	if policy.defaultID != domain.DefaultDivisionID {
		t.Fatalf("default shard = %q, want %q", policy.defaultID, domain.DefaultDivisionID)
	}
	if len(policy.allowedIDs) < 2 {
		t.Fatalf("allowed ids = %v, want at least global-official and test", policy.allowedIDs)
	}
	resolve := DevResolveDivisionID(
		StaticCharacterSource{},
		policy.allowedIDs,
		policy.defaultID,
	)
	if got := resolve(domain.TestDivisionID); got != domain.TestDivisionID {
		t.Fatalf("resolve(test) = %q, want %q", got, domain.TestDivisionID)
	}
}
