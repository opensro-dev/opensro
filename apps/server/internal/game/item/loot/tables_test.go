/*
===========================================================================

tables_test.go - the operator's loot tables for this package's tests

TestMain publishes the tables from source-data/server/loot before any test
runs, and keeps their bytes for tests that read the sources directly.
requireTables skips a test that needs them when they are absent.

===========================================================================
*/
package loot

import (
	"os"
	"testing"

	"opensro.online/server/internal/testsupport/licensed"
)

// The table sources as the operator's source-data holds them; nil without it.
var propertiesJSON, equipmentJSON, consumablesJSON []byte

/*
================
TestMain
================
*/
func TestMain(m *testing.M) {
	var data [len(TableFiles)][]byte
	complete := true
	for i, name := range TableFiles {
		bytes, ok, err := licensed.ReadSourceData("server/" + name)
		if err != nil {
			panic(err)
		}
		complete = complete && ok
		data[i] = bytes
	}
	if complete {
		if err := LoadTables(data[0], data[1], data[2]); err != nil {
			panic(err)
		}
		propertiesJSON, equipmentJSON, consumablesJSON = data[0], data[1], data[2]
	}
	os.Exit(m.Run())
}

/*
================
requireTables

Skips the test (or fails it under SRO_REQUIRE_GAME_DATA=1) without the tables.
================
*/
func requireTables(t testing.TB) {
	t.Helper()
	if Loaded() {
		return
	}
	if os.Getenv(licensed.RequireEnv) == "1" {
		t.Fatalf("loot tables are not in source-data/server/loot (%s=1 requires them)", licensed.RequireEnv)
	}
	t.Skip("loot tables are not in source-data/server/loot")
}
