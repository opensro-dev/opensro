/*
===========================================================================

tomb_tables_test.go - the operator's Qin-Shi Tomb tables for this package's tests

The tomb's tables are source-data, never in the repository. TestMain
publishes them before any test runs; requireQinShiTomb skips a test that
needs them when they are absent.

===========================================================================
*/
package monster

import (
	"os"
	"testing"

	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestMain
================
*/
func TestMain(m *testing.M) {
	names := [8]string{"characterdata.tsv", "nests.tsv", "hives.tsv", "tactics.json",
		"skilldata.tsv", "teleportdata.tsv", "teleportlink.tsv", "eventzone.tsv"}
	var data [8][]byte
	complete := true
	for i, name := range names {
		bytes, ok, err := licensed.ReadSourceData("server/" + QinShiTombDir + "/" + name)
		if err != nil {
			panic(err)
		}
		complete = complete && ok
		data[i] = bytes
	}
	if complete {
		InstallQinShiTomb(&QinShiTombTables{
			CharacterData: string(data[0]), Nests: string(data[1]), Hives: string(data[2]), Tactics: data[3],
			SkillData: string(data[4]), TeleportData: string(data[5]), TeleportLink: string(data[6]), EventZone: string(data[7]),
		})
	}
	os.Exit(m.Run())
}

/*
================
requireQinShiTomb

Skips the test (or fails it under SRO_REQUIRE_GAME_DATA=1) without the tables.
================
*/
func requireQinShiTomb(t testing.TB) {
	t.Helper()
	if qinshiTomb != nil {
		return
	}
	if os.Getenv(licensed.RequireEnv) == "1" {
		t.Fatalf("Qin-Shi Tomb tables are not in source-data/server/%s (%s=1 requires them)", QinShiTombDir, licensed.RequireEnv)
	}
	t.Skipf("Qin-Shi Tomb tables are not in source-data/server/%s", QinShiTombDir)
}
