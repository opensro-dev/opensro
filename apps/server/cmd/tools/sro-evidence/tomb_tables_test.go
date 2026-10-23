/*
===========================================================================

tomb_tables_test.go - the operator's Qin-Shi Tomb tables for this package's tests

The tomb's tables are source-data, never in the repository. TestMain
publishes them before any test runs; tests that need them call
qinshitest.Require.

===========================================================================
*/
package main

import (
	"os"
	"testing"

	"opensro.online/server/internal/testsupport/qinshitest"
)

/*
================
TestMain
================
*/
func TestMain(m *testing.M) {
	qinshitest.LoadForTests()
	os.Exit(m.Run())
}
