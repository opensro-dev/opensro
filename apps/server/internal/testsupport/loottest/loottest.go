/*
===========================================================================

loottest.go - the loot catalogue's tables for tests

The tables are the operator's source-data (source-data/server/loot), never in
the repository. LoadForTests runs from a package's TestMain, before any test,
so no test reads the catalogue while it is published. Require then skips a
test that needs the tables when they were not available (or fails it under
SRO_REQUIRE_GAME_DATA=1).

===========================================================================
*/
package loottest

import (
	"os"
	"testing"

	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
LoadForTests

Publishes the operator's tables when every one is present. Call it from
TestMain; an unreadable or invalid table is fatal there.
================
*/
func LoadForTests() {
	var data [len(loot.TableFiles)][]byte
	for i, name := range loot.TableFiles {
		bytes, ok, err := licensed.ReadSourceData("server/" + name)
		if err != nil {
			panic(err)
		}
		if !ok {
			return
		}
		data[i] = bytes
	}
	if err := loot.LoadTables(data[0], data[1], data[2]); err != nil {
		panic(err)
	}
}

/*
================
Require

Skips the test (or fails it under SRO_REQUIRE_GAME_DATA=1) unless
LoadForTests published the tables.
================
*/
func Require(t testing.TB) {
	t.Helper()
	if loot.Loaded() {
		return
	}
	if os.Getenv(licensed.RequireEnv) == "1" {
		t.Fatalf("loot tables are not in source-data/server/loot (%s=1 requires them)", licensed.RequireEnv)
	}
	t.Skip("loot tables are not in source-data/server/loot; generate them with scripts/build/generate_loot_catalog.py")
}
