/*
===========================================================================

authority_upgrade_layouts_test.go - every older layout the current
schema admits upgrades

The offline upgrader validates a source at its own layout before it adds
the missing steps' tables. r204j's upgrade of the live layout-14
authority failed there: a layout-15 table was probed by a validator that
ran from layout 5 on (validateMallAccounts, validateSilkHistory). These
tests upgrade a source at each older layout, and the production shape
(schema 49 at layout 14), so a validator that reads a later layout's
table fails here, not on the host.

===========================================================================
*/
package store

import (
	"fmt"
	"testing"

	"opensro.online/server/internal/domain"
)

// The live authority before r204: schema 49 at layout 14 (4f111d998).
const (
	liveSchemaBeforeEarnedSilk = 49
	liveLayoutBeforeEarnedSilk = 14
)

/*
================
TestUpgradeFromEveryOlderLayout
================
*/
func TestUpgradeFromEveryOlderLayout(t *testing.T) {
	oldest := storeSteps[CurrentVersion].minLayout
	for layout := oldest; layout < CurrentLayoutVersion; layout++ {
		t.Run(fmt.Sprintf("layout%d", layout), func(t *testing.T) {
			upgradeAndReopen(t, CurrentVersion, layout)
		})
	}
}

/*
================
TestUpgradeFromTheLiveAuthorityBeforeEarnedSilk

The exact shape the r204 deploy upgrades on every enabled shard.
================
*/
func TestUpgradeFromTheLiveAuthorityBeforeEarnedSilk(t *testing.T) {
	upgradeAndReopen(t, liveSchemaBeforeEarnedSilk, liveLayoutBeforeEarnedSilk)
}

/*
================
upgradeAndReopen

A store stamped at schema and layout, upgraded with commit, then opened
by the current binary with its character intact.
================
*/
func upgradeAndReopen(t *testing.T, schema, layout int) {
	t.Helper()
	dir := t.TempDir()
	name := olderLayoutStore(t, dir, newTestClock(), layout)
	rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, schema)
	if backup, err := UpgradeAuthority(dir, true); err != nil || backup == "" {
		t.Fatalf("upgrade from schema %d layout %d: backup %q: %v", schema, layout, backup, err)
	}
	reopened := openTest(t, dir, newTestClock())
	defer reopened.Close()
	found := false
	reopened.ReadCharacters(testDivision, func(characters []*domain.Character) {
		for _, c := range characters {
			found = found || c.Name == name
		}
	})
	if !found {
		t.Fatalf("character %q missing after the upgrade from schema %d layout %d", name, schema, layout)
	}
}
