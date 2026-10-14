/*
===========================================================================

zerocap_test.go - standalone nests capped at zero take their monster's cap

The shard backup behind the population evidence zeroes single nests of
monsters it keeps live beside them (the Bandit Stronghold building).
restoreZeroCapNests gives them the cap of the monster's live nests; a hive
member keeps its zero, since its hive caps it.

===========================================================================
*/
package monster

import (
	"testing"

	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestRestoreZeroCapNestsPrefersTheSameRegion
================
*/
func TestRestoreZeroCapNestsPrefersTheSameRegion(t *testing.T) {
	refs := map[uint32]MonsterRef{1: {Codename: "MOB_A"}, 2: {Codename: "MOB_B"}}
	nest := func(ref uint32, region uint16, cap int) NestRow {
		return NestRow{SpawnPoint: SpawnPoint{RefObjID: ref, RegionID: region}, RetailEvidence: true, MaxCount: cap}
	}
	nests := []NestRow{
		nest(1, 10, 8), nest(1, 10, 8), nest(1, 10, 12), nest(1, 20, 3), // MOB_A live: 8, 8, 12 here; 3 elsewhere
		nest(1, 10, 0), // same region: most common 8
		nest(1, 30, 0), // no live nest here: most common anywhere (8)
		nest(2, 10, 6), nest(2, 11, 9),
		nest(2, 12, 0), // tie 6/9 anywhere: the larger
	}
	hive := nest(1, 10, 0)
	hive.HiveMaxCount = 4
	nests = append(nests, hive)
	unmatched := NestRow{SpawnPoint: SpawnPoint{RefObjID: 1, RegionID: 10}}
	nests = append(nests, unmatched)
	restoreZeroCapNests(refs, nests)
	for i, want := range []int{8, 8, 12, 3, 8, 8, 6, 9, 9, 0, 0} {
		if nests[i].MaxCount != want {
			t.Errorf("nest %d: cap %d, want %d", i, nests[i].MaxCount, want)
		}
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
