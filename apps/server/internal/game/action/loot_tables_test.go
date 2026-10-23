/*
===========================================================================

loot_tables_test.go - the operator's loot tables for this package's tests

The loot catalogue's tables are source-data, never in the repository.
TestMain publishes them before any test runs; tests that need them call
loottest.Require.

===========================================================================
*/
package action

import (
	"os"
	"testing"

	"opensro.online/server/internal/testsupport/loottest"
)

/*
================
TestMain
================
*/
func TestMain(m *testing.M) {
	loottest.LoadForTests()
	os.Exit(m.Run())
}
