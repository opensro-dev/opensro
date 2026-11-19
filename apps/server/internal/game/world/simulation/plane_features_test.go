/*
===========================================================================

plane_features_test.go - the per-plane feature table, pinned

===========================================================================
*/
package simulation

import "testing"

/*
================
TestPlaneFeaturesPinTheCaveColumn

Outdoors every feature is on; in a cave only dormancy is, which sleeps by
message block there (monster_dormancy.go). The rest are exactly the gates
these fields replaced (#1646).
================
*/
func TestPlaneFeaturesPinTheCaveColumn(t *testing.T) {
	outdoor := PlaneFeatures{QuestMonsterSpawns: true, CaravanBandits: true, UniqueReturnAnchors: true, Dormancy: true,
		SpawnTerrainLift: true, SpawnStrandedRescue: true, QuestGatherAreas: true, HelpCallSight: true}
	if got := FeaturesAt(0x62a8); got != outdoor {
		t.Errorf("outdoor features %+v, want %+v", got, outdoor)
	}
	for _, cave := range []uint16{0x8001, 0x8004} {
		// A cave sleeps by message block (monster_dormancy.go); every
		// other outdoor feature stays off.
		if got := FeaturesAt(cave); got != (PlaneFeatures{Dormancy: true, DormancyByBlock: true}) {
			t.Errorf("cave %#x features %+v, want block dormancy only", cave, got)
		}
	}
}
