/*
===========================================================================

paramdefinitions_main_test.go - the parameter definitions for this package's tests

The keepers clamp by the parameter definition table, which is source-data.
TestMain installs the operator's table, or the test-authored one without it
(paramtest), before any test runs.

===========================================================================
*/
package progression

import (
	"os"
	"testing"

	"opensro.online/server/internal/testsupport/paramtest"
)

/*
================
TestMain
================
*/
func TestMain(m *testing.M) {
	paramtest.InstallForTests()
	os.Exit(m.Run())
}
