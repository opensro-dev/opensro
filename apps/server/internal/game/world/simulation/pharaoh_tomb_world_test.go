/*
===========================================================================

pharaoh_tomb_world_test.go - the three tombs as separate permanent worlds.

===========================================================================
*/
package simulation

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestDivisionOpensRegisteredTombWorlds

#1750: with the tomb worlds registered (the boot composition does it
while SRO_PHARAOH_TOMB is on), a division opens each tomb's permanent
world at boot and each population owns only its own nests; the default
world is unchanged.
================
*/
func TestDivisionOpensRegisteredTombWorlds(t *testing.T) {
	if err := instance.RegisterPortOnly(instance.PharaohTombDefinitions()); err != nil {
		t.Fatal(err)
	}
	definitions := instance.PharaohTombDefinitions()
	nests := []monster.NestRow{
		{WorldCode: "INS_DEFAULT", SpawnPoint: monster.SpawnPoint{RegionID: 1}},
		{WorldCode: definitions[0].CodeName, SpawnPoint: monster.SpawnPoint{RegionID: 29631}},
		{WorldCode: definitions[1].CodeName, SpawnPoint: monster.SpawnPoint{RegionID: 31423}},
		{WorldCode: definitions[2].CodeName, SpawnPoint: monster.SpawnPoint{RegionID: 32000}},
	}
	s := NewMonsterState(monster.TemplateFromParts(nil, nests))
	s.SetTimeSource(func() time.Time { return time.UnixMilli(1) })
	s.StartDivision("a")
	for index, definition := range definitions {
		lease, ok := s.PopulationLease("a", instance.Pack(definition.ID, 1))
		if !ok {
			t.Fatalf("tomb world %s not open at boot", definition.CodeName)
		}
		state := s.worldPopulations[populationKey{"a", lease}]
		if state == nil || len(state.nests) != 1 || state.nests[index+1] == nil {
			t.Fatalf("tomb world %s does not own exactly its nest: %+v", definition.CodeName, state)
		}
	}
	state := s.divs["a"]
	if len(state.nests) != 1 || state.nests[0] == nil {
		t.Fatalf("default population owns more than its own nest: %+v", state.nests)
	}
}
