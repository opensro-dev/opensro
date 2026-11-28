/*
===========================================================================

fortress_structure_appearance_test.go - the structures' standing
skills ride their create rows

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
attachedSkill

The skill a structure's projection attaches, if any.
================
*/
func attachedSkill(rt *Runtime, refObjID uint32) (uint32, bool) {
	for _, structure := range rt.Monsters.WorldStructures(testDivision, instance.Pack(2, 1)) {
		if structure.Ref.RefObjID != refObjID {
			continue
		}
		def := simulation.MonsterWireDefFromInstance(structure, 0)
		for effect := range def.LinkedEffects.Entries() {
			return effect.SkillID, true
		}
		return 0, false
	}
	return 0, false
}

/*
================
TestFortressStructuresShowTheirStandingSkills

No war has started: the appearance is not a war act. A standing tower
and the fort stone carry their authored skills in their projections,
so a viewer's create row seeds them; the fallen tower's list empties
while the stone keeps its own.
================
*/
func TestFortressStructuresShowTheirStandingSkills(t *testing.T) {
	rt, _, _ := pulseFixture(t)
	// Open the division's fortress table, as the boot does.
	rt.Fortresses.Records(testDivision)
	if _, attached := attachedSkill(rt, 19536); attached {
		t.Fatal("the tower shows a skill before the appearance lane runs")
	}
	rt.advanceFortressStructureAppearances()
	if skill, attached := attachedSkill(rt, 19536); !attached || skill != 20530 {
		t.Fatalf("tower skill = %d, %v; want 20530 attached", skill, attached)
	}
	if skill, attached := attachedSkill(rt, 19553); !attached || skill != 25046 {
		t.Fatalf("stone skill = %d, %v; want 25046 attached", skill, attached)
	}
	// Again: the list is kept, not stacked.
	rt.advanceFortressStructureAppearances()
	for _, structure := range rt.Monsters.WorldStructures(testDivision, instance.Pack(2, 1)) {
		if got := simulation.MonsterWireDefFromInstance(structure, 0).LinkedEffects.Len(); got != 1 {
			t.Fatalf("structure %d carries %d effects, want 1", structure.Ref.RefObjID, got)
		}
	}
	fellStructure(t, rt, structureKindGuardTower)
	rt.advanceFortressStructureAppearances()
	if _, attached := attachedSkill(rt, 19536); attached {
		t.Fatal("the fallen tower still shows its skill")
	}
	if _, attached := attachedSkill(rt, 19553); !attached {
		t.Fatal("the standing stone lost its skill")
	}
}
