/*
===========================================================================

loot_tables_test.go - the operator's derived tables for this package's tests

The loot catalogue's tables are source-data, never in the repository.
TestMain publishes them before any test runs; tests that need them call
loottest.Require. It installs the parameter definitions too (paramtest).

===========================================================================
*/
package action

import (
	"os"
	"testing"

	"opensro.online/server/internal/testsupport/loottest"
	"opensro.online/server/internal/testsupport/paramtest"
	"opensro.online/server/internal/testsupport/qinshitest"
)

/*
================
TestMain
================
*/
func TestMain(m *testing.M) {
	loottest.LoadForTests()
	qinshitest.LoadForTests()
	paramtest.InstallForTests()
	os.Exit(m.Run())
}
