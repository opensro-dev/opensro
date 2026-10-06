/*
===========================================================================

population_settled_test.go - when the boot fill counts as settled

===========================================================================
*/

package simulation

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestPopulationSettlesOneNestTickAfterTheLastSpawn

Before any population pass the division is not settled; after a pass that
places monsters, neither elapsed time nor a pass inside one native nest
tick settles it; a completed pass a full nest tick later that places
nothing does. A division without population is settled.
================
*/
func TestPopulationSettlesOneNestTickAfterTheLastSpawn(t *testing.T) {
	s := NewMonsterState(monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{1: {TidWord: 0x00C6, RefObjID: 1, Name: "Nest", MaxHP: 100}},
		[]monster.NestRow{{SpawnPoint: monster.SpawnPoint{RefObjID: 1, RegionID: 257, X: 12, Z: 34}, MaxCount: 1, PolicyPinned: true}},
	))
	now := time.Unix(100, 0)
	s.SetTimeSource(func() time.Time { return now })
	s.StartDivision("a")
	if s.PopulationSettled("a") {
		t.Fatal("settled before any population pass")
	}
	s.AdvancePopulation(s.CurrentTimeMillis())
	if len(s.InstancesInRegions("a", []uint16{257})) != 1 {
		t.Fatal("the boot pass placed nothing")
	}
	if s.PopulationSettled("a") {
		t.Fatal("settled in the pass that spawned")
	}
	// Time alone settles nothing: only a completed pass a nest tick later.
	now = now.Add(time.Duration(monster.NestHiveTickMs) * time.Millisecond)
	if s.PopulationSettled("a") {
		t.Fatal("settled on elapsed time before the next pass ran")
	}
	s.AdvancePopulation(s.CurrentTimeMillis() - 1)
	if s.PopulationSettled("a") {
		t.Fatal("a pass inside one nest tick of the last spawn settled the fill")
	}
	s.AdvancePopulation(s.CurrentTimeMillis())
	if !s.PopulationSettled("a") {
		t.Fatal("a full nest tick without a spawn did not settle the fill")
	}
	if !s.PopulationSettled("absent") {
		t.Fatal("a division without population is not settled")
	}
}
