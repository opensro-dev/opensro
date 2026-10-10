/*
===========================================================================

skilltrapfield_test.go - Poison Trap planting, pulses and expiry

Drive the production cast, release and skill-object tick owners with the
shipped Poison Trap row and the combat fixture's monster.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
trapFieldPulses

The planter's pulse results (B3C6 mode 2) in one tick's batches.
================
*/
func trapFieldPulses(t *testing.T, out []simulation.DivisionFrames, planter, skill uint32) int {
	t.Helper()
	pulses := 0
	for _, batch := range out {
		for _, f := range batch.Frames {
			if f.Opcode != wire.OpSkillPulse {
				continue
			}
			if len(f.Payload) < 9 || f.Payload[0] != 2 || binary.LittleEndian.Uint32(f.Payload[1:]) != planter ||
				binary.LittleEndian.Uint32(f.Payload[5:]) != skill {
				t.Fatalf("invalid trap field result: %x", f.Payload)
			}
			pulses++
		}
	}
	return pulses
}

/*
================
TestPoisonTrapPulsesOnItsPeriod

The release plants a visible object with no board instance. The first pass
with an enemy in range strikes at once (the pulse clock starts at zero),
passes inside the period do not, the next strike follows the period, and
the object retires after its dura.
================
*/
func TestPoisonTrapPulsesOnItsPeriod(t *testing.T) {
	rt, clock, c, skill, _ := plantedTrapFixture(t, "SKILL_EU_ROG_POISONA_FIELD_A_01")
	release := plantCombatTrap(t, rt, clock, c, skill, clock.NowMs())
	objects := rt.SkillObjects.Snapshot()
	if len(objects) != 1 || !objects[0].Program.Pulse || objects[0].Program.Combat || objects[0].Program.Hidden ||
		objects[0].OwnerEffect != 0 || objects[0].Program.PulseMs != skill.TrapField.PulseMs {
		t.Fatalf("planting failed: %+v", objects)
	}
	if buffs := rt.effects.Snapshot(testDivision, c.Name); len(buffs) != 0 {
		t.Fatalf("a trap field installed a board instance: %+v", buffs)
	}
	sessions := []simulation.SessionView{{
		DivisionID: testDivision, CharacterID: c.ID, Population: objects[0].Population,
		PublishedObjects: []uint32{}, World: simulation.SeedWorldState(c),
	}}
	planter := enterworld.ObjectIDForCharacter(c)
	scan := int64(enterworld.CombatTrapScanMs)
	first := release + scan
	if n := trapFieldPulses(t, rt.AdvanceSkillObjects(first, sessions), planter, skill.ID); n != 1 {
		t.Fatalf("first pass struck %d times, want 1", n)
	}
	if n := trapFieldPulses(t, rt.AdvanceSkillObjects(first+scan, sessions), planter, skill.ID); n != 0 {
		t.Fatal("a pass inside the period struck")
	}
	second := first + int64(skill.TrapField.PulseMs)
	if n := trapFieldPulses(t, rt.AdvanceSkillObjects(second, sessions), planter, skill.ID); n != 1 {
		t.Fatalf("the period's pass struck %d times, want 1", n)
	}
	if len(rt.SkillObjects.Snapshot()) != 1 {
		t.Fatal("a trap field retired on a strike")
	}
	rt.AdvanceSkillObjects(release+int64(skill.TrapField.DurationMs)+1, sessions)
	if len(rt.SkillObjects.Snapshot()) != 0 {
		t.Fatal("the trap field outlived its dura")
	}
}

/*
================
TestPoisonTrapWaitsForAnEnemy

With nobody in range nothing is struck and the pulse clock keeps its
last strike, so the first enemy is struck on the pass it is seen.
================
*/
func TestPoisonTrapWaitsForAnEnemy(t *testing.T) {
	rt, clock, c, skill, gid := plantedTrapFixture(t, "SKILL_EU_ROG_POISONA_FIELD_A_01")
	if !rt.Monsters.Defeat(testDivision, gid, clock.Now()) {
		t.Fatal("could not clear the fixture monster")
	}
	release := plantCombatTrap(t, rt, clock, c, skill, clock.NowMs())
	if out := rt.AdvanceSkillObjects(release+int64(enterworld.CombatTrapScanMs), nil); trapFieldPulses(t, out, enterworld.ObjectIDForCharacter(c), skill.ID) != 0 {
		t.Fatal("an empty trap field struck")
	}
	if objects := rt.SkillObjects.Snapshot(); len(objects) != 1 || objects[0].LastPulseMs != 0 {
		t.Fatalf("an empty pass moved the pulse clock: %+v", objects)
	}
}
