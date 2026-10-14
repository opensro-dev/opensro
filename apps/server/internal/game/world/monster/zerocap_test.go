/*
===========================================================================

zerocap_test.go - standalone nests capped at zero take their ISRO-R cap

The shard backup behind the population evidence zeroes single nests of
monsters it keeps live beside them (the Bandit Stronghold building).
restoreZeroCapNests gives them the live cap ISRO-R's Tab_RefNest holds for
the same nest; a hive member keeps its zero, since its hive caps it.

===========================================================================
*/
package monster

import (
	"testing"

	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestRestoreZeroCapNestsTakesTheISRORCap

A standalone zero-cap nest takes the cap the table names for its anchor; a
hive member and a nest without evidence keep theirs, and a zero-cap nest
the table lacks stops the load rather than guessing.
================
*/
func TestRestoreZeroCapNestsTakesTheISRORCap(t *testing.T) {
	refs := map[uint32]MonsterRef{1: {Codename: "MOB_A"}}
	nest := func(x float64, cap int) NestRow {
		return NestRow{SpawnPoint: SpawnPoint{RefObjID: 1, RegionID: 10, X: x, Y: 5, Z: 7}, RetailEvidence: true, MaxCount: cap}
	}
	caps := map[populationEvidenceKey]int{evidenceKey("MOB_A", 10, 1, 5, 7): 6}
	hive := nest(2, 0)
	hive.HiveMaxCount = 4
	nests := []NestRow{nest(1, 0), nest(3, 9), hive, {SpawnPoint: SpawnPoint{RefObjID: 1, RegionID: 10, X: 4}}}
	restoreZeroCapNests(refs, nests, caps)
	for i, want := range []int{6, 9, 0, 0} {
		if nests[i].MaxCount != want {
			t.Errorf("nest %d: cap %d, want %d", i, nests[i].MaxCount, want)
		}
	}
	defer func() {
		if recover() == nil {
			t.Fatal("a zero-cap nest without an ISRO-R cap loaded")
		}
	}()
	restoreZeroCapNests(refs, []NestRow{nest(8, 0)}, caps)
}

/*
================
TestZeroCapEvidenceCoversEveryRow

Every generated row parses to a positive cap.
================
*/
func TestZeroCapEvidenceCoversEveryRow(t *testing.T) {
	if caps := loadZeroCapCaps(zeroCapCapsTSV); len(caps) != 173 {
		t.Fatalf("%d zero-cap evidence rows, want 173", len(caps))
	}
}

/*
================
TestShippedBanditStrongholdIsPopulated

Thread 1559583744489291979: every nest in the Bandit Stronghold building
(0x5BA0) spawns, and no standalone evidence nest is left at zero.
================
*/
func TestShippedBanditStrongholdIsPopulated(t *testing.T) {
	template := LoadTemplate(gamedatatest.TextdataDir(t))
	stronghold := 0
	for _, nest := range template.Nests {
		if nest.RetailEvidence && nest.MaxCount == 0 && nest.HiveMaxCount == 0 {
			t.Errorf("%s at %#x still capped at zero", template.Refs[nest.RefObjID].Codename, nest.RegionID)
		}
		if nest.RegionID == 0x5BA0 {
			stronghold++
			if nest.MaxCount <= 0 && nest.HiveMaxCount <= 0 {
				t.Errorf("Stronghold nest %s spawns nothing", template.Refs[nest.RefObjID].Codename)
			}
		}
	}
	if stronghold != 19 {
		t.Fatalf("%d nests in the Stronghold region", stronghold)
	}
}
