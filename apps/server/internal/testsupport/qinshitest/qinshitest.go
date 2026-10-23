/*
===========================================================================

qinshitest.go - the Qin-Shi Tomb tables for tests

The tomb's tables are the operator's source-data (source-data/server/
qinshi-tomb), never in the repository. LoadForTests runs from a package's
TestMain, before any test, so no test reads them while they are published.
Require then skips a test that needs them when they were not available (or
fails it under SRO_REQUIRE_GAME_DATA=1).

===========================================================================
*/
package qinshitest

import (
	"os"
	"testing"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
LoadForTests

Publishes the operator's tomb tables when all of them are present. Call it
from TestMain; an unreadable table is fatal there.
================
*/
func LoadForTests() {
	names := [8]string{"characterdata.tsv", "nests.tsv", "hives.tsv", "tactics.json",
		"skilldata.tsv", "teleportdata.tsv", "teleportlink.tsv", "eventzone.tsv"}
	var data [8][]byte
	for i, name := range names {
		bytes, ok, err := licensed.ReadSourceData("server/" + monster.QinShiTombDir + "/" + name)
		if err != nil {
			panic(err)
		}
		if !ok {
			return
		}
		data[i] = bytes
	}
	monster.InstallQinShiTomb(&monster.QinShiTombTables{
		CharacterData: string(data[0]), Nests: string(data[1]), Hives: string(data[2]), Tactics: data[3],
		SkillData: string(data[4]), TeleportData: string(data[5]), TeleportLink: string(data[6]), EventZone: string(data[7]),
	})
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
	if monster.QinShiTomb() != nil {
		return
	}
	if os.Getenv(licensed.RequireEnv) == "1" {
		t.Fatalf("Qin-Shi Tomb tables are not in source-data/server/%s (%s=1 requires them)", monster.QinShiTombDir, licensed.RequireEnv)
	}
	t.Skipf("Qin-Shi Tomb tables are not in source-data/server/%s", monster.QinShiTombDir)
}
