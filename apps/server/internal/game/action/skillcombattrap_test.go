/*
===========================================================================

skillcombattrap_test.go - Fire Trap planting, triggering and retirement

Drive the production cast, release and skill-object tick owners with the
shipped Fire Trap row and the combat fixture's monster.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
combatTrapFixture

A Wizard standing on the fixture monster, holding the authored staff.
================
*/
func combatTrapFixture(t *testing.T) (*Runtime, *fakeClock, *enterworld.Character, enterworld.SkillRow, uint32) {
	t.Helper()
	rt, clock, c, target := newCombatTestRuntime(t, 1000000)
	skill := shippedOffense(t, "SKILL_EU_WIZARD_FIREA_TRAP_A_01")
	if !skill.CombatTrap.Pinned {
		t.Fatalf("Fire Trap not admitted: %+v", skill.CombatTrap)
	}
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.RaceIndex = testInt64(enterworld.RaceEurope)
	c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	c.Skills = []uint32{skill.ID}
	c.Intellect = testInt64(2000)
	c.CurrentMP = testInt64(100000)
	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = int64(skill.RequiredWeaponKinds[0])
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	// Planting needs the planter's admitted population (the monster's).
	if err := rt.admitPopulationSession(testDivision, c.Name, 1); err != nil {
		t.Fatal(err)
	}
	mover, ok := rt.Monsters.Mover(testDivision, target.Gid)
	if !ok {
		t.Fatal("fixture monster has no mover")
	}
	pose := mover.LivePoseAt(clock.NowMs(), nil)
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(w *simulation.WorldState) {
			w.Spawn = simulation.Spawn{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z}
			w.SpawnSet = true
		})
	return rt, clock, c, skill, target.Gid
}

/*
================
plantCombatTrap

Cast, then release at the prepared boundary; returns the release instant.
================
*/
func plantCombatTrap(t *testing.T, rt *Runtime, clock *fakeClock, c *enterworld.Character, skill enterworld.SkillRow, at int64) int64 {
	t.Helper()
	out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
	if frame, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok || frame.Payload[0] != 1 {
		t.Fatalf("Fire Trap refused: %+v", out)
	}
	release := at + int64(skill.ActionCastingTimeMs) + 1
	if len(rt.advanceProjectileCasts(release)) == 0 {
		t.Fatal("Fire Trap never released")
	}
	return release
}

/*
================
TestFireTrapExplodesOnTheFirstMonsterInRange

The planted object is hidden from others, strikes the monster through a
B3C6 pulse credited to its planter, and retires after one explosion.
================
*/
func TestFireTrapExplodesOnTheFirstMonsterInRange(t *testing.T) {
	rt, clock, c, skill, gid := combatTrapFixture(t)
	before, _ := rt.Monsters.Get(testDivision, gid)
	mp := enterworld.CurrentMP(c)
	release := plantCombatTrap(t, rt, clock, c, skill, clock.NowMs())
	objects := rt.SkillObjects.Snapshot()
	if len(objects) != 1 || !objects[0].Program.Combat || !objects[0].Program.Hidden ||
		objects[0].OwnerGID != enterworld.ObjectIDForCharacter(c) || enterworld.CurrentMP(c) >= mp {
		t.Fatalf("planting failed: %+v MP %d -> %d", objects, mp, enterworld.CurrentMP(c))
	}
	trapGID := objects[0].Spawn.GID
	if buffs := rt.effects.Snapshot(testDivision, c.Name); len(buffs) != 1 || buffs[0].InstanceToken != objects[0].OwnerEffect {
		t.Fatalf("live trap missing from the planter's buff board: %+v", buffs)
	}
	// The release installs nothing on the caster; its own close retires the aura.
	closed := false
	for _, batch := range rt.drainSkillFinalizes(release + int64(skill.ActionDurationMs)) {
		for _, f := range batch.Frames {
			closed = closed || f.Opcode == wire.OpSkillEffectControl && len(f.Payload) == 6 && f.Payload[0] == 2
		}
	}
	if !closed {
		t.Fatal("planting never closed its cast bracket")
	}

	out := rt.AdvanceSkillObjects(release+int64(enterworld.CombatTrapScanMs), nil)
	after, _ := rt.Monsters.Get(testDivision, gid)
	if after.CurrentHP >= before.CurrentHP {
		t.Fatalf("explosion dealt no damage: HP %d -> %d", before.CurrentHP, after.CurrentHP)
	}
	if len(rt.SkillObjects.Snapshot()) != 0 || len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
		t.Fatal("exploded trap or its buff was not retired")
	}
	found := false
	for _, batch := range out {
		for _, f := range batch.Frames {
			if f.Opcode == wire.OpSkillPulse && len(f.Payload) >= 9 &&
				binary.LittleEndian.Uint32(f.Payload[1:]) == enterworld.ObjectIDForCharacter(c) &&
				binary.LittleEndian.Uint32(f.Payload[5:]) == skill.ID {
				found = true
			}
		}
	}
	if !found || trapGID == 0 {
		t.Fatalf("no B3C6 result from the planter: %+v", out)
	}
}

/*
================
TestFireTrapRetirement

A second plant retires the first (one live trap per link group) and its
buff, and a planter walking past the authored distance retires both.
================
*/
func TestFireTrapRetirement(t *testing.T) {
	rt, clock, c, skill, gid := combatTrapFixture(t)
	if !rt.Monsters.Defeat(testDivision, gid, clock.Now()) {
		t.Fatal("could not clear the trigger monster")
	}
	plantCombatTrap(t, rt, clock, c, skill, clock.NowMs())
	first := rt.SkillObjects.Snapshot()[0].Spawn.GID
	clock.Advance(time.Duration(skill.CoolTimeMs+1) * time.Millisecond)
	rt.drainSkillFinalizes(clock.NowMs())
	release := plantCombatTrap(t, rt, clock, c, skill, clock.NowMs())
	objects := rt.SkillObjects.Snapshot()
	if len(objects) != 1 || objects[0].Spawn.GID == first {
		t.Fatalf("live trap cap not enforced: %+v", objects)
	}
	if buffs := rt.effects.Snapshot(testDivision, c.Name); len(buffs) != 1 || buffs[0].InstanceToken != objects[0].OwnerEffect {
		t.Fatalf("replaced trap left its buff: %+v", buffs)
	}
	key := simulation.WorldKey(testDivision, c.Name)
	rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) {
		w.Spawn.X += float64(skill.CombatTrap.OwnerDistance) + 1
	})
	rt.AdvanceSkillObjects(release+int64(enterworld.CombatTrapScanMs), nil)
	if len(rt.SkillObjects.Snapshot()) != 0 || len(rt.effects.Snapshot(testDivision, c.Name)) != 0 {
		t.Fatal("trap or its buff survived its planter walking away")
	}
}

/*
================
TestFireTrapLimitIsScopedToDivision

Player GIDs may coincide across divisions. Replanting must neither count
nor retire another division's trap, even when its link group also matches.
================
*/
func TestFireTrapLimitIsScopedToDivision(t *testing.T) {
	rt, clock, c, skill, gid := combatTrapFixture(t)
	if !rt.Monsters.Defeat(testDivision, gid, clock.Now()) {
		t.Fatal("could not clear the trigger monster")
	}
	plantCombatTrap(t, rt, clock, c, skill, clock.NowMs())
	foreign := rt.SkillObjects.Snapshot()[0]
	foreign.Division = "other-division"
	foreign, err := rt.SkillObjects.Create(foreign)
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Duration(skill.CoolTimeMs+1) * time.Millisecond)
	rt.drainSkillFinalizes(clock.NowMs())
	plantCombatTrap(t, rt, clock, c, skill, clock.NowMs())
	objects := rt.SkillObjects.Snapshot()
	if len(objects) != 2 {
		t.Fatalf("replant crossed division boundary: %+v", objects)
	}
	found := false
	for _, object := range objects {
		found = found || object.Spawn.GID == foreign.Spawn.GID
	}
	if !found {
		t.Fatal("another division's trap was retired")
	}
}
